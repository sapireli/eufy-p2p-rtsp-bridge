package com.eufywall.tv

import android.app.AlertDialog
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.StateListDrawable
import android.widget.ImageView
import android.widget.Toast
import android.app.Activity
import android.app.AlarmManager
import android.app.PendingIntent
import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.net.wifi.WifiManager
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.Process
import android.os.SystemClock
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.GridLayout
import android.widget.LinearLayout
import android.widget.TextView
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.net.InetAddress
import java.net.URI
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.UUID

data class Camera(val sn: String, val name: String, val rtsp: String, val mode: String, val streaming: Boolean,
                  val dual: Boolean, val dualView: String?, val width: Int, val height: Int,
                  val rtspTcpPacketSize: Int? = null,
                  val hasStill: Boolean = false,
                  internal val freshness: FreshnessCapability? = null,
                  internal val nativeRendition: NativeRendition? = null,
                  internal val nativeDecoderName: String? = null)

class MainActivity : Activity() {
    private val ui = Handler(Looper.getMainLooper())
    private val work = Executors.newSingleThreadExecutor()
    private val http = OkHttpClient.Builder().connectTimeout(4, TimeUnit.SECONDS).readTimeout(5, TimeUnit.SECONDS).build()
    private val prefs by lazy { getSharedPreferences("wall", MODE_PRIVATE) }
    private val owner by lazy { prefs.getString("owner", null) ?: UUID.randomUUID().toString().also { prefs.edit().putString("owner", it).apply() } }
    private var bridge = ""
    private var cameras = listOf<Camera>()
    private val chosen = linkedSetOf<String>() // Legacy preference retained for older APKs.
    private var wallConfig = WallConfiguration()
    private var wallPolicy = WallPolicy()
    private val resolvedTiles = mutableMapOf<String, WallResolvedTile>()
    private val images = mutableMapOf<String, ImageView>()
    private val snapshotKeys = mutableMapOf<String, String>()
    private val snapshotVersions = mutableMapOf<String, Long>()
    private val held = linkedSetOf<String>()
    private val snapshots by lazy { SnapshotLoader(http) }
    private val holds by lazy { CameraHoldClient(http) }
    private val wallTick = object : Runnable {
        override fun run() {
            if (!wallVisible) return
            refreshWall()
            ui.postDelayed(this, 1_000)
        }
    }
    private val states = mutableMapOf<String, String>()
    private val players = mutableMapOf<String, LowLatencyRtspPlayer>()
    private val playbackContracts = mutableMapOf<String, PlaybackContract>()
    private val nativeFallback = NativeFallback()
    private var nativeProfileProbeUntil = 0L
    private val frameWatchdogs = mutableMapOf<String, Runnable>()
    private val labels = mutableMapOf<String, TextView>()
    private val surfaces = mutableMapOf<String, FrameLayout>()
    private var socket: WebSocket? = null
    private var wallVisible = false
    private var editingWall = false
    private var connectionSettings = false
    private var activityStarted = false
    private var resumeWallOnStart = false
    private var cameraLoadGeneration = 0L
    private var cameraLoadInFlight = false
    private var savedWallBridge: String? = null
    private var destroyed = false
    private val savedWallRetry = Runnable {
        if (activityStarted && !wallVisible) savedWallBridge?.let { loadCameras(it, true) }
    }
    private var nsd: NsdManager? = null
    private var discovery: NsdManager.DiscoveryListener? = null
    private var multicast: WifiManager.MulticastLock? = null
    private var status: TextView? = null
    private var ipEntry: EditText? = null
    private var decoderFailures = 0
    private var decoderRecoveryScheduled = false
    private val reconnect = Runnable { if (wallVisible) connectEvents() }
    private val controlRecovery = ControlChannelRecovery(
        reportLoss = { reason -> android.util.Log.i("EufyWallTV", "Control socket lost reason=$reason; preserving ${players.size} RTSP players; reconnect in 5000ms") },
        scheduleReconnect = { ui.removeCallbacks(reconnect); ui.postDelayed(reconnect, 5_000) },
    )
    private val firstFrameTimeoutMs = 15_000L
    private val liveFrameTimeoutMs = 5_000L
    private val holdRefresh = object : Runnable {
        override fun run() {
            if (!wallVisible) return
            refreshWall()
            held.toList().forEach { hold(it, "POST") }
            ui.postDelayed(this, 20_000)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val probeSeconds = if (BuildConfig.DEBUG) intent.getIntExtra("nativeProfileProbeSeconds", 0).coerceIn(0, 120) else 0
        if (probeSeconds > 0) {
            nativeProfileProbeUntil = SystemClock.elapsedRealtime() + probeSeconds * 1000L
            android.util.Log.w("EufyWallTV", "DEBUG native profile probe active for ${probeSeconds}s; reported profile eligibility bypassed, bitstream unchanged")
            ui.postDelayed({
                nativeProfileProbeUntil = 0L
                if (wallVisible) { players.keys.toList().forEach { release(it, "native-profile-probe-expired") }; refreshWall() }
                android.util.Log.i("EufyWallTV", "DEBUG native profile probe expired; conservative selection restored")
            }, probeSeconds * 1000L)
        }
        window.decorView.systemUiVisibility = View.SYSTEM_UI_FLAG_FULLSCREEN or View.SYSTEM_UI_FLAG_HIDE_NAVIGATION or View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
        bridge = prefs.getString("bridge", "") ?: ""
        chosen.addAll((prefs.getString("chosen", "") ?: "").split(',').filter { it.isNotBlank() }.take(36))
        wallConfig = WallConfiguration.decode(prefs.getString("configuration", null))
            ?: WallConfiguration.fromLegacyChosen(chosen.toList())
        showSetup()
        if (bridge.isNotBlank()) loadCameras(bridge, true) else startDiscovery()
    }

    private fun text(value: String, size: Float = 22f): TextView = TextView(this).apply {
        this.text = value; textSize = size; setTextColor(0xffffffff.toInt()); setPadding(16, 10, 16, 10)
    }

    private fun button(value: String, action: () -> Unit): Button = Button(this).apply {
        text = value; textSize = 17f; isAllCaps = false
        setTextColor(0xffedf5fc.toInt()); setPadding(20, 10, 20, 10)
        background = StateListDrawable().apply {
            addState(intArrayOf(android.R.attr.state_focused), rounded(0xff2367a0.toInt()))
            addState(intArrayOf(android.R.attr.state_pressed), rounded(0xff2367a0.toInt()))
            addState(intArrayOf(), rounded(0xff203044.toInt()))
        }
        setOnClickListener { action() }
    }

    private fun rounded(color: Int) = GradientDrawable().apply {
        setColor(color); cornerRadius = 12f; setStroke(2, 0xff40556d.toInt())
    }

    private fun setLabel(label: TextView, value: String) {
        if (label.text.toString() != value) label.text = value
    }

    private fun saveConfiguration(next: WallConfiguration): Boolean {
        val errors = next.validationErrors()
        if (errors.isNotEmpty()) {
            val message = if (errors.any { it.contains("grid", true) || it.contains("Tile ") }) "These views do not fit. Choose a larger layout or smaller view sizes." else errors.first()
            Toast.makeText(this, message, Toast.LENGTH_LONG).show(); return false
        }
        wallConfig = next
        chosen.clear(); chosen.addAll(next.tiles.filterNot { it.spec.motion }.map { it.spec.camera })
        prefs.edit().putString("configuration", next.encode()).putString("chosen", chosen.joinToString(",")).apply()
        return true
    }

    private fun setupPage(title: String, message: String): LinearLayout {
        cancelCameraLoad(); resumeWallOnStart = false; wallVisible = false; stopWall()
        ipEntry = null
        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL; setPadding(48, 28, 48, 28)
            setBackgroundColor(0xff101c27.toInt())
        }
        root.addView(text(title, 34f))
        status = text(message, 17f).also { root.addView(it) }
        setContentView(android.widget.ScrollView(this).apply {
            isFillViewport = true; setBackgroundColor(0xff101c27.toInt()); addView(root)
        })
        return root
    }

    private fun focusFirst(view: View) {
        ui.post { if (!wallVisible && activityStarted && !destroyed && view.isAttachedToWindow) view.requestFocus() }
    }

    private fun showSetup(message: String = "") {
        if (cameras.isEmpty()) { showConnectionSettings(if (message.isBlank()) "Find your bridge or enter its address." else message); return }
        editingWall = false; connectionSettings = false
        val root = setupPage("Eufy Wall", if (message.isBlank()) {
            if (wallConfig.tiles.isEmpty()) "Choose the cameras you want to watch." else "${wallConfig.tiles.size} camera views ready"
        } else message)
        val watch = button(if (wallConfig.tiles.isEmpty()) "Choose cameras" else "Watch cameras") {
            if (wallConfig.tiles.isEmpty()) chooseCameras() else showWall()
        }.apply { textSize = 24f }
        root.addView(watch, LinearLayout.LayoutParams(-1, -2).apply { setMargins(0, 20, 0, 12) })
        root.addView(button("Edit wall") { showWallEditor() }, LinearLayout.LayoutParams(-1, -2))
        root.addView(text("Your cameras", 24f))
        wallConfig.tiles.take(5).forEach { tile -> root.addView(text(viewName(tile), 20f)) }
        if (wallConfig.tiles.size > 5) root.addView(text("And ${wallConfig.tiles.size - 5} more", 18f))
        if (wallConfig.tiles.isEmpty()) root.addView(text("Add the cameras you want to watch. Battery cameras stay asleep until they wake or you choose Wake.", 18f))
        root.addView(button("Connection settings") { showConnectionSettings("Connected to ${bridge.removePrefix("http://")}") },
            LinearLayout.LayoutParams(-1, -2).apply { setMargins(0, 24, 0, 0) })
        focusFirst(watch)
    }

    private fun showConnectionSettings(message: String = "") {
        editingWall = false; connectionSettings = true
        val root = setupPage("Bridge connection", message)
        root.addView(text("Bridge address", 20f))
        ipEntry = EditText(this).apply {
            setSingleLine(true); hint = "homebridge.lan"; setText(bridge.removePrefix("http://"))
            setTextColor(0xffffffff.toInt()); setHintTextColor(0xffaabbcc.toInt())
            inputType = android.text.InputType.TYPE_CLASS_TEXT
        }.also { root.addView(it) }
        val actions = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
        val connect = button("Connect") {
            val value = ipEntry?.text.toString().trim()
            if (value.isNotEmpty()) try { loadCameras(normalize(value), false) }
            catch (_: Exception) { status?.text = "Enter a valid bridge address." }
        }.apply { id = View.generateViewId() }
        actions.addView(connect); actions.addView(button("Find automatically") { startDiscovery() }); root.addView(actions)
        ipEntry?.nextFocusDownId = connect.id
        ipEntry?.setOnKeyListener { _, code, event ->
            if (code == android.view.KeyEvent.KEYCODE_DPAD_DOWN) {
                if (event.action == android.view.KeyEvent.ACTION_DOWN) connect.requestFocus()
                true
            } else false
        }
        if (cameras.isNotEmpty()) root.addView(button("Back to wall") { showSetup() })
        focusFirst(connect)
    }

    private fun normalize(input: String): String {
        val raw = if (input.contains("://")) input else "http://$input"
        val uri = URI(raw)
        val host = uri.host ?: error("enter an IP address or hostname")
        return "http://${if (host.contains(':')) "[$host]" else host}:${if (uri.port > 0) uri.port else 3000}"
    }

    private fun loadCameras(url: String, autoOpen: Boolean) {
        if (destroyed) return
        cancelCameraLoad()
        if (autoOpen && wallConfig.tiles.isNotEmpty()) savedWallBridge = url
        val generation = cameraLoadGeneration
        cameraLoadInFlight = true
        stopDiscovery()
        status?.text = "Connecting to $url…"
        work.execute {
            try {
                val request = Request.Builder().url("$url/api/cameras").build()
                val body = http.newCall(request).execute().use { response ->
                    if (!response.isSuccessful) error("HTTP ${response.code}")
                    response.body?.string() ?: error("Empty response")
                }
                val parsed = JSONArray(body)
                val list = (0 until parsed.length()).map { parsed.getJSONObject(it) }
                    .filter { it.optBoolean("enabled") }
                    .map {
                        val f = it.optJSONObject("freshness")?.takeIf { f -> freshnessMode(f.optInt("version"), if(f.has("mode")) f.optString("mode") else null) != null && f.optString("scope") == "bridge-ingress" && f.optInt("maxAgeMs") in 100..60000 }
                        Camera(it.getString("sn"), it.optString("name", it.getString("sn")), it.getString("rtsp"), it.optString("mode", "always"), it.optBoolean("streaming"), it.optBoolean("dual"), if (it.isNull("dualView")) null else it.optString("dualView").ifBlank { null }, it.optInt("width"), it.optInt("height"), it.optInt("rtspTcpPacketSize").takeIf { size -> size in 256..65535 }, it.optBoolean("still", it.optString("mode") != "always"), f?.let { f -> FreshnessCapability(f.getInt("maxAgeMs"), f.getString("clockUrl"), freshnessMode(f.getInt("version"), if(f.has("mode")) f.optString("mode") else null)!!) }, parseNativeRendition(it.optJSONObject("nativeRendition")))
                    }
                ui.post {
                    if (destroyed || generation != cameraLoadGeneration) return@post
                    cameraLoadInFlight = false
                    if (!activityStarted) return@post
                    if (autoOpen && wallConfig.tiles.isNotEmpty() && list.isEmpty()) {
                        scheduleSavedWallRetry("Bridge is starting • retrying saved wall")
                        return@post
                    }
                    bridge = url; cameras = list; prefs.edit().putString("bridge", url).apply()
                    states.clear(); list.forEach { states[it.sn] = if (it.streaming) "live" else "idle" }
                    wallPolicy.applyHello(list.map { WallCameraState(it.sn, it.name, it.mode, states[it.sn] ?: "idle", it.hasStill) })
                    if (autoOpen && wallConfig.tiles.isNotEmpty() && list.isNotEmpty()) showWall()
                    else showSetup("Bridge connected · ${list.size} cameras available")
                }
            } catch (e: Exception) {
                ui.post {
                    if (destroyed || generation != cameraLoadGeneration) return@post
                    cameraLoadInFlight = false
                    if (!activityStarted) return@post
                    status?.text = "Bridge unavailable: ${e.message}"
                    if (autoOpen && wallConfig.tiles.isNotEmpty()) scheduleSavedWallRetry("Bridge unavailable • retrying saved wall")
                }
            }
        }
    }

    private fun cancelCameraLoad(clearSavedWall: Boolean = true) {
        cameraLoadGeneration++
        cameraLoadInFlight = false
        ui.removeCallbacks(savedWallRetry)
        if (clearSavedWall) savedWallBridge = null
    }

    private fun scheduleSavedWallRetry(message: String) {
        if (!activityStarted || wallVisible || savedWallBridge == null || destroyed) return
        status?.text = message
        android.util.Log.i("EufyWallTV", "Saved wall camera discovery retry in 5000ms")
        ui.removeCallbacks(savedWallRetry)
        ui.postDelayed(savedWallRetry, 5_000)
    }

    private fun viewName(tile: ConfiguredWallTile): String = if (tile.spec.motion) "Motion view" else
        cameras.firstOrNull { it.sn == tile.spec.camera }?.name ?: "Unavailable camera"

    private fun showWallEditor(message: String = "Choose a camera below to change it.") {
        editingWall = true; connectionSettings = false
        val root = setupPage("Edit wall", message)
        val done = button("Done") { showSetup() }
        root.addView(done)
        val actions = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
        actions.addView(button("Choose cameras") { chooseCameras() })
        actions.addView(button("Add motion view") { chooseWatched(null) })
        actions.addView(button("Layout") { editLayout() }); root.addView(actions)
        root.addView(text(if (wallConfig.layout == WallLayoutMode.AUTO) "Automatic layout" else "${wallConfig.columns} columns · ${wallConfig.rows} rows", 18f))
        wallConfig.tiles.forEachIndexed { index, tile ->
            val description = if (tile.spec.motion) {
                if (tile.spec.watch.isEmpty()) "Shows the camera with the most recent motion" else
                    "Follows motion from " + cameras.filter { it.sn in tile.spec.watch }.joinToString { it.name }
            } else if (cameras.firstOrNull { it.sn == tile.spec.camera }?.mode == "always") "Live camera" else "Shows a snapshot while asleep"
            root.addView(button("${index + 1}.  ${viewName(tile)}\n$description") { editTile(tile) }.apply {
                gravity = Gravity.START or Gravity.CENTER_VERTICAL
            }, LinearLayout.LayoutParams(-1, -2).apply { setMargins(0, 8, 0, 0) })
        }
        if (wallConfig.tiles.isEmpty()) root.addView(text("Choose cameras to create your wall.", 20f))
        focusFirst(done)
    }

    private fun chooseCameras() {
        val selected = wallConfig.tiles.filterNot { it.spec.motion }.map { it.spec.camera }.toMutableSet()
        AlertDialog.Builder(this).setTitle("Choose cameras")
            .setMultiChoiceItems(cameras.map { it.name }.toTypedArray(), cameras.map { it.sn in selected }.toBooleanArray()) { _, i, checked ->
                if (checked) selected.add(cameras[i].sn) else selected.remove(cameras[i].sn)
            }.setNegativeButton("Cancel", null).setPositiveButton("Apply") { _, _ ->
                try {
                    val next = WallEditing.withCameras(wallConfig, selected.toList()) { UUID.randomUUID().toString() }
                    if (saveConfiguration(next)) showWallEditor("Cameras saved")
                } catch (e: IllegalArgumentException) { Toast.makeText(this, e.message, Toast.LENGTH_LONG).show() }
            }.show()
    }

    private fun chooseWatched(tile: ConfiguredWallTile?) {
        val selected = if (tile == null || tile.spec.watch.isEmpty()) cameras.map { it.sn }.toMutableSet() else tile.spec.watch.toMutableSet()
        AlertDialog.Builder(this).setTitle(if (tile == null) "Follow motion from these cameras" else "Watched cameras")
            .setMultiChoiceItems(cameras.map { it.name }.toTypedArray(), cameras.map { it.sn in selected }.toBooleanArray()) { _, i, checked ->
                if (checked) selected.add(cameras[i].sn) else selected.remove(cameras[i].sn)
            }.setNegativeButton("Cancel", null).setPositiveButton(if (tile == null) "Add view" else "Apply", null).create().apply {
                setOnShowListener { getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
                    if (selected.isEmpty()) { Toast.makeText(this@MainActivity, "Choose at least one camera.", Toast.LENGTH_SHORT).show(); return@setOnClickListener }
                    val watch = if (selected.containsAll(cameras.map { it.sn })) emptyList() else selected.toList()
                    val updated = if (tile == null) ConfiguredWallTile(WallTileSpec(UUID.randomUUID().toString(), motion = true, watch = watch))
                        else tile.copy(spec = tile.spec.copy(watch = watch))
                    val list = if (tile == null) wallConfig.tiles + updated else wallConfig.tiles.map { if (it.spec.id == tile.spec.id) updated else it }
                    if (saveConfiguration(wallConfig.copy(tiles = list))) { dismiss(); showWallEditor("Motion view saved") }
                } }; show()
            }
    }

    private fun applyTile(tile: ConfiguredWallTile, updated: ConfiguredWallTile, message: String = "View saved") {
        if (saveConfiguration(wallConfig.copy(tiles = wallConfig.tiles.map { if (it.spec.id == tile.spec.id) updated else it }))) showWallEditor(message)
    }

    private fun editTile(tile: ConfiguredWallTile) {
        val options = mutableListOf<Pair<String, () -> Unit>>()
        if (tile.spec.motion) {
            options += "Watched cameras" to { chooseWatched(tile) }
            options += "Minimum display time: ${if (tile.spec.dwellMs == 0L) "Switch immediately" else "${tile.spec.dwellMs / 1000} seconds"}" to {
                chooseDuration("Stay on a camera for", listOf(0, 5000, 10000, 15000, 30000), "Switch immediately", tile.spec.dwellMs) { applyTile(tile, tile.copy(spec = tile.spec.copy(dwellMs = it))) }
            }
            options += "Hide after quiet: ${if (tile.spec.blankAfterMs == 0L) "Never" else "${tile.spec.blankAfterMs / 1000} seconds"}" to {
                chooseDuration("Hide when there is no new motion", listOf(0, 15000, 30000, 60000, 120000), "Keep the last camera visible", tile.spec.blankAfterMs) { applyTile(tile, tile.copy(spec = tile.spec.copy(blankAfterMs = it))) }
            }
        } else {
            options += "Change camera" to {
                AlertDialog.Builder(this).setTitle("Show this camera").setItems(cameras.map { it.name }.toTypedArray()) { _, i -> applyTile(tile, tile.copy(spec = tile.spec.copy(camera = cameras[i].sn))) }.show()
            }
            val cam = cameras.firstOrNull { it.sn == tile.spec.camera }
            if (cam?.dual == true) options += "Camera image: ${if (cam.dualView?.startsWith("pip-") == true) "Picture in picture" else "Stacked views"}" to {
                AlertDialog.Builder(this).setTitle("Camera image").setItems(arrayOf("Stacked views", "Picture in picture")) { _, i ->
                    if ((i == 1) != (cam.dualView?.startsWith("pip-") == true)) setDualView(cam.sn, null)
                }.show()
            }
        }
        val index = wallConfig.tiles.indexOfFirst { it.spec.id == tile.spec.id }
        fun move(delta: Int) {
            val tiles = wallConfig.tiles.toMutableList(); val current = tiles.indexOfFirst { it.spec.id == tile.spec.id }
            if (current < 0 || current + delta !in tiles.indices) return
            val moved = tiles.removeAt(current); tiles.add(current + delta, moved)
            if (saveConfiguration(wallConfig.copy(tiles = tiles))) showWallEditor("Order saved")
        }
        if (index > 0) options += "Move earlier" to { move(-1) }
        if (index in 0 until wallConfig.tiles.lastIndex) options += "Move later" to { move(1) }
        if (wallConfig.layout == WallLayoutMode.GRID) options += "Size (advanced)" to { editSize(tile) }
        options += "Remove from wall" to {
            if (saveConfiguration(wallConfig.copy(tiles = wallConfig.tiles.filterNot { it.spec.id == tile.spec.id }))) showWallEditor("View removed")
        }
        AlertDialog.Builder(this).setTitle(viewName(tile)).setItems(options.map { it.first }.toTypedArray()) { _, i -> options[i].second() }.setNegativeButton("Back", null).show()
    }

    private fun chooseDuration(title: String, values: List<Long>, zero: String, current: Long, selected: (Long) -> Unit) {
        val labels = values.map { if (it == 0L) zero else "${it / 1000} seconds" }
        AlertDialog.Builder(this).setTitle(title).setSingleChoiceItems(labels.toTypedArray(), values.indexOf(current)) { dialog, i -> dialog.dismiss(); selected(values[i]) }.setNegativeButton("Cancel", null).show()
    }

    private fun editLayout() {
        AlertDialog.Builder(this).setTitle("Choose a layout")
            .setItems(arrayOf("Automatic · fits your cameras", "2 columns", "3 columns", "4 columns", "6 columns", "Custom grid (advanced)")) { _, i ->
                if (i == 5) { editCustomGrid(); return@setItems }
                try {
                    val next = if (i == 0) wallConfig.copy(layout = WallLayoutMode.AUTO) else WallEditing.presetColumns(wallConfig, listOf(2, 3, 4, 6)[i - 1])
                    if (saveConfiguration(next)) showWallEditor("Layout saved")
                } catch (_: IllegalArgumentException) { Toast.makeText(this, "Choose more columns to fit these cameras.", Toast.LENGTH_LONG).show() }
            }.setNegativeButton("Cancel", null).show()
    }

    private fun numberChoice(title: String, max: Int, current: Int, selected: (Int) -> Unit) {
        AlertDialog.Builder(this).setTitle(title).setSingleChoiceItems((1..max).map { it.toString() }.toTypedArray(), current - 1) { dialog, i -> dialog.dismiss(); selected(i + 1) }.setNegativeButton("Cancel", null).show()
    }

    private fun editCustomGrid() {
        var columns = wallConfig.columns; var rows = wallConfig.rows
        val body = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(28, 16, 28, 16) }
        val col = button("Columns: $columns") {}; col.setOnClickListener { numberChoice("Columns", 6, columns) { columns = it; col.text = "Columns: $columns" } }; body.addView(col)
        val row = button("Rows: $rows") {}; row.setOnClickListener { numberChoice("Rows", 6, rows) { rows = it; row.text = "Rows: $rows" } }; body.addView(row)
        AlertDialog.Builder(this).setTitle("Custom grid").setView(body).setNegativeButton("Cancel", null).setPositiveButton("Apply", null).create().apply {
            setOnShowListener { getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
                if (saveConfiguration(wallConfig.copy(layout = WallLayoutMode.GRID, columns = columns, rows = rows))) { dismiss(); showWallEditor("Layout saved") }
            } }; show()
        }
    }

    private fun editSize(tile: ConfiguredWallTile) {
        var width = tile.columnSpan; var height = tile.rowSpan
        val body = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(28, 16, 28, 16) }
        val w = button("Width: $width columns") {}; w.setOnClickListener { numberChoice("Width in columns", wallConfig.columns, width) { width = it; w.text = "Width: $width columns" } }; body.addView(w)
        val h = button("Height: $height rows") {}; h.setOnClickListener { numberChoice("Height in rows", wallConfig.rows, height) { height = it; h.text = "Height: $height rows" } }; body.addView(h)
        AlertDialog.Builder(this).setTitle("View size").setView(body).setNegativeButton("Cancel", null).setPositiveButton("Apply", null).create().apply {
            setOnShowListener { getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
                val updated = tile.copy(columnSpan = width, rowSpan = height)
                val next = wallConfig.copy(tiles = wallConfig.tiles.map { if (it.spec.id == tile.spec.id) updated else it })
                if (saveConfiguration(next)) { dismiss(); showWallEditor("Size saved") }
            } }; show()
        }
    }

    private fun viewLabel(cam: Camera) = "${cam.name} view: ${if (cam.dualView?.startsWith("pip-") == true) "PiP" else "Split"} (switch)"

    private fun setDualView(sn: String, control: Button?) {
        val cam = cameras.firstOrNull { it.sn == sn } ?: return
        val next = if (cam.dualView?.startsWith("pip-") == true) "split" else "pip-br"
        control?.isEnabled = false
        status?.text = "Switching ${cam.name} to ${if (next == "split") "Split" else "PiP"}…"
        work.execute {
            try {
                val url = "$bridge/api/cameras/$sn/view?mode=$next"
                http.newCall(Request.Builder().url(url).post(ByteArray(0).toRequestBody(null)).build()).execute().use { response ->
                    if (!response.isSuccessful) error(response.body?.string() ?: "HTTP ${response.code}")
                }
                ui.post {
                    cameras = cameras.map { if (it.sn == sn) it.copy(dualView = next, width = 0, height = 0) else it }
                    control?.text = viewLabel(cameras.first { it.sn == sn })
                    if (editingWall) showWallEditor("Camera image saved") else status?.text = "Camera image saved"
                    control?.isEnabled = true
                }
            } catch (e: Exception) { ui.post { status?.text = "Could not switch ${cam.name}: ${e.message}"; control?.isEnabled = true } }
        }
    }

    private fun showWall() {
        if (!activityStarted) { resumeWallOnStart = true; return }
        cancelCameraLoad()
        if (bridge.isBlank() || wallConfig.tiles.isEmpty()) return
        stopDiscovery(); stopWall(); wallVisible = true
        wallPolicy.applyHello(cameras.map { WallCameraState(it.sn, it.name, it.mode, states[it.sn] ?: "idle", it.hasStill) })
        val portraits = cameras.filter { if (it.dualView?.startsWith("pip-") == true || it.dualView == "single") false else if (it.width > 0 && it.height > 0) it.height > it.width else it.dualView == "split" }.map { it.sn }.toSet()
        val bounds = WallLayout.calculate(wallConfig, portraits)
        val wall = WallCanvas(this, bounds)
        bounds.forEach { position ->
            val id = position.tile.spec.id
            val tile = FrameLayout(this).apply { setBackgroundColor(0xff000000.toInt()); isFocusable = true; isClickable = true; keepScreenOn = true }
            val image = ImageView(this).apply { scaleType = ImageView.ScaleType.FIT_CENTER; visibility = View.GONE }
            val video = FrameLayout(this); tile.addView(video, FrameLayout.LayoutParams(-1, -1)); surfaces[id] = video
            tile.addView(image, FrameLayout.LayoutParams(-1, -1)); images[id] = image
            val label = text("Waiting for motion", 16f).apply { setBackgroundColor(0xaa000000.toInt()) }
            tile.addView(label, FrameLayout.LayoutParams(-1, -2, Gravity.BOTTOM)); labels[id] = label
            tile.setOnFocusChangeListener { _, focused -> tile.foreground = if (focused) GradientDrawable().apply { setColor(0x00000000); setStroke(4, 0xff55b6ff.toInt()) } else null }
            tile.setOnClickListener { showLiveActions(id) }
            wall.addView(tile)
        }
        setContentView(wall); refreshWall(); ui.postDelayed(wallTick, 1_000); ui.postDelayed(holdRefresh, 20_000); connectEvents()
    }

    private fun showLiveActions(id: String) {
        val sn = resolvedTiles[id]?.camera
        val cam = cameras.firstOrNull { it.sn == sn }
        val options = if (cam != null && cam.mode != "always") arrayOf("Wake for 2 minutes", "Edit wall") else arrayOf("Edit wall")
        AlertDialog.Builder(this).setTitle(cam?.name ?: "Motion tile").setItems(options) { _, which ->
            if (options[which] == "Edit wall") showWallEditor()
            else if (cam != null) { wallPolicy.requestWake(cam.sn, SystemClock.elapsedRealtime(), 120_000); refreshWall() }
        }.show()
    }

    private fun refreshWall() {
        if (!wallVisible) return
        val next = wallPolicy.resolve(wallConfig.tiles.map { it.spec }, SystemClock.elapsedRealtime())
        next.forEach { selection ->
            val previous = resolvedTiles[selection.id]
            if (previous?.camera != selection.camera) {
                release(selection.id, "tile-camera-changed"); snapshots.cancel(selection.id); snapshotKeys.remove(selection.id)
                images[selection.id]?.setImageDrawable(null)
            }
            if (previous?.content == WallContent.LIVE && selection.content != WallContent.LIVE) snapshotKeys.remove(selection.id)
            resolvedTiles[selection.id] = selection
            updateTile(selection.id)
        }
        val needed = next.filter { it.hold }.mapNotNull { it.camera }.toSet()
        (held - needed).forEach { hold(it, "DELETE") }
        (needed - held).forEach { hold(it, "POST") }
        held.clear(); held.addAll(needed)
    }

    private fun wantsLive(id: String) = wallVisible && resolvedTiles[id]?.content == WallContent.LIVE

    private fun snapshotFor(id: String, cam: Camera) {
        val key = "${cam.sn}:${snapshotVersions[cam.sn] ?: 0L}"
        if (snapshotKeys[id] == key) return
        snapshotKeys[id] = key
        snapshots.load(id, "$bridge/snapshot/${cam.sn}?v=${snapshotVersions[cam.sn] ?: 0L}") { bitmap ->
            if (!wallVisible || resolvedTiles[id]?.camera != cam.sn || players[id]?.decodedFrames?.get()?.let { it > 0 } == true) return@load
            images[id]?.apply {
                if (bitmap == null) setImageDrawable(null) else setImageBitmap(bitmap)
                visibility = if (bitmap != null) View.VISIBLE else View.GONE
            }
        }
    }

    private fun updateTile(sn: String) {
        if (decoderRecoveryScheduled) return
        val selection = resolvedTiles[sn] ?: return
        val cam = cameras.firstOrNull { it.sn == selection.camera }
        val label = labels[sn] ?: return
        if (cam == null) {
            release(sn, "tile-empty"); snapshots.cancel(sn); snapshotKeys.remove(sn)
            images[sn]?.apply { setImageDrawable(null); visibility = View.GONE }
            setLabel(label, if (wallConfig.tiles.firstOrNull { it.spec.id == sn }?.spec?.motion == true) "Waiting for motion" else "Camera unavailable")
            return
        }
        if (selection.content == WallContent.LIVE) {
            if (!players.containsKey(sn) && cam.mode != "always") snapshotFor(sn, cam)
            if (players.containsKey(sn)) {
                if ((players[sn]?.decodedFrames?.get() ?: 0) > 0) {
                    snapshots.cancel(sn); snapshotKeys.remove(sn); images[sn]?.visibility = View.GONE
                }
                return
            }
            val playback = selectPlayback(cam)
            val hardware = playback.nativeDecoderName
            android.util.Log.i("EufyWallTV", "Rendition $sn: ${if (playback.nativeDecoderName != null) "native HEVC hardware=$hardware ${playback.width}x${playback.height}" else "compatibility (native absent, unsupported, or failed)"}")
            label.text = "${cam.name} • connecting"
            val startedAt = SystemClock.elapsedRealtime()
            lateinit var player: LowLatencyRtspPlayer
            var retryScheduled = false
            fun retry(message: String, decoder: Boolean) {
                if (!wallVisible || players[sn] !== player || retryScheduled) return
                retryScheduled = true
                label.text = "${cam.name} • $message, reconnecting"
                android.util.Log.w("EufyWallTV", "RTSP ${cam.sn}: $message; ${player.diagnostics()}")
                if (playback.nativeDecoderName != null) {
                    nativeFallback.fail(cam.sn)
                    android.util.Log.w("EufyWallTV", "Native rendition failed for $sn; switching to compatibility URL: $message")
                } else if (decoder) {
                    decoderFailures++
                    if (decoderFailures >= 2) { recoverDecoder(); return }
                }
                ui.postDelayed({
                    if (wantsLive(sn) && players[sn] === player) {
                        release(sn, "rtsp-retry"); updateTile(sn)
                    }
                }, 1_000)
            }
            player = LowLatencyRtspPlayer(this, playback,
                onFirstFrame = {
                    if (players[sn] === player) {
                        snapshots.cancel(sn); snapshotKeys.remove(sn); images[sn]?.visibility = View.GONE
                        label.text = if(playback.freshness?.enforces == true) "${cam.name} • capture time unverified" else cam.name
                        decoderFailures = 0
                        android.util.Log.i("EufyWallTV", "Rendered first frame for ${cam.sn} after ${SystemClock.elapsedRealtime() - startedAt}ms; ${player.diagnostics()}")
                    }
                },
                onFailure = { message, decoder -> retry(message, decoder) },
                onFreshnessState = { message -> if (players[sn] === player) label.text = "${cam.name} • ${message ?: "capture age unverified"}" })
            players[sn] = player
            playbackContracts[sn] = playback.playbackContract()
            surfaces[sn]?.addView(player.view, FrameLayout.LayoutParams(-1, -1))
            var lastLogAt = 0L
            val decoderWatchdog = DecoderProgressWatchdog(firstFrameTimeoutMs, liveFrameTimeoutMs)
            val watchdog = object : Runnable {
                override fun run() {
                    if (!wantsLive(sn) || players[sn] !== player) return
                    val now = SystemClock.elapsedRealtime()
                    // Decoder output is reliable progress in every freshness mode.
                    // OnFrameRendered is informational and can be delayed, batched,
                    // or omitted on older TVs; its absence cannot prove a freeze.
                    val stalledForMs = decoderWatchdog.stalledForMs(now, startedAt,
                        player.decodedFrames.get(), player.lastDecodedFrameAt.get())
                    if (now - lastLogAt >= 5_000) {
                        android.util.Log.i("EufyWallTV", "Live RTSP ${cam.sn}: ${player.diagnostics()}")
                        lastLogAt = now
                    }
                    if (stalledForMs != null) {
                        retry("No decoder output for ${stalledForMs}ms", false)
                    } else {
                        ui.postDelayed(this, 1_000)
                    }
                }
            }
            frameWatchdogs[sn] = watchdog
            ui.postDelayed(watchdog, 1_000)
        } else {
            release(sn, "camera-content:${selection.content}")
            if (selection.content == WallContent.SNAPSHOT) {
                images[sn]?.visibility = if (images[sn]?.drawable != null) View.VISIBLE else View.GONE
                snapshotFor(sn, cam)
            }
            else { snapshots.cancel(sn); images[sn]?.visibility = View.GONE }
            setLabel(label, "${cam.name} • ${if (selection.starting) "starting" else "asleep"}${if (images[sn]?.drawable != null) " · last snapshot" else ""}")

        }
    }

    private fun selectPlayback(camera: Camera): Camera = selectedPlayback(camera, nativeFallback.permits(camera.sn)) {
        nativeHardwareDecoder(it, BuildConfig.DEBUG && SystemClock.elapsedRealtime() < nativeProfileProbeUntil)
    }

    private fun releaseChangedPlayback(camera: Camera, reason: String) {
        val tiles = resolvedTiles.filterValues { it.camera == camera.sn }.keys.filter { players.containsKey(it) }
        if (tiles.isEmpty()) return
        val selected = selectPlayback(camera).playbackContract()
        tiles.filter { playbackContracts[it] != selected }.forEach { release(it, reason) }
    }

    private fun release(sn: String, reason: String) {
        playbackContracts.remove(sn)
        frameWatchdogs.remove(sn)?.let { ui.removeCallbacks(it) }
        val player = players.remove(sn) ?: return
        android.util.Log.i("EufyWallTV", "Player released sn=$sn reason=$reason decodedFrames=${player.decodedFrames.get()} receivedUnits=${player.receivedUnits.get()}")
        player.release()
    }

    private fun recoverDecoder() {
        if (decoderRecoveryScheduled) return
        decoderRecoveryScheduled = true
        players.keys.toList().forEach { release(it, "decoder-recovery") }
        val last = prefs.getLong("decoder_restart_at", 0)
        val now = System.currentTimeMillis()
        if (now - last < 5 * 60_000) {
            labels.values.forEach { it.text = "Hardware decoder busy • retrying" }
            android.util.Log.e("EufyWallTV", "Decoder recovery already attempted recently")
            ui.postDelayed({
                if (wallVisible) {
                    decoderRecoveryScheduled = false
                    decoderFailures = 0
                    refreshWall()
                }
            }, 30_000)
            return
        }
        prefs.edit().putLong("decoder_restart_at", now).commit()
        labels.values.forEach { it.text = "Restarting video decoder…" }
        val intent = packageManager.getLaunchIntentForPackage(packageName) ?: return
        val pending = PendingIntent.getActivity(
            this, 1, intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        val alarms = getSystemService(Context.ALARM_SERVICE) as AlarmManager
        val whenToRestart = SystemClock.elapsedRealtime() + 2_000
        try { alarms.setExact(AlarmManager.ELAPSED_REALTIME_WAKEUP, whenToRestart, pending) }
        catch (_: SecurityException) { alarms.set(AlarmManager.ELAPSED_REALTIME_WAKEUP, whenToRestart, pending) }
        android.util.Log.w("EufyWallTV", "Restarting app to release stuck hardware decoders")
        ui.postDelayed({ Process.killProcess(Process.myPid()) }, 250)
    }

    private fun connectEvents() {
        if (!wallVisible) return
        socket?.cancel(); socket = null
        val url = bridge.replaceFirst("http://", "ws://") + "/ws"
        socket = http.newWebSocket(Request.Builder().url(url).build(), object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                try {
                    val event = JSONObject(text)
                    ui.post {
                        if (!wallVisible || socket !== webSocket) return@post
                        when (event.optString("type")) {
                            "hello" -> {
                                val arr = event.optJSONArray("cameras") ?: JSONArray()
                                for (i in 0 until arr.length()) {
                                    val item = arr.getJSONObject(i)
                                    val sn = item.getString("sn")
                                    states[sn] = item.optString("state", "idle")
                                    val cap = item.optJSONObject("freshness")?.takeIf { f -> freshnessMode(f.optInt("version"), if(f.has("mode")) f.optString("mode") else null) != null && f.optString("scope") == "bridge-ingress" && f.optInt("maxAgeMs") in 100..60000 }
                                        ?.let { f -> FreshnessCapability(f.getInt("maxAgeMs"), f.getString("clockUrl"), freshnessMode(f.getInt("version"), if(f.has("mode")) f.optString("mode") else null)!!) }
                                    cameras = cameras.map { camera ->
                                        if (camera.sn != sn) camera else {
                                            val updated = camera.copy(name = item.optString("name", camera.name), mode = item.optString("mode", camera.mode),
                                                hasStill = item.optBoolean("still"), rtsp = item.optString("rtsp", camera.rtsp), freshness = cap,
                                                nativeRendition = parseNativeRendition(item.optJSONObject("nativeRendition")))
                                            if (updated.rtsp != camera.rtsp || updated.freshness != camera.freshness || updated.nativeRendition != camera.nativeRendition)
                                                releaseChangedPlayback(updated, "hello-metadata-changed")
                                            updated
                                        }
                                    }
                                }
                                val present = (0 until arr.length()).map { arr.getJSONObject(it).getString("sn") }.toSet()
                                wallPolicy.applyHello(cameras.filter { it.sn in present }.map { camera ->
                                    val item = (0 until arr.length()).map { arr.getJSONObject(it) }.first { it.getString("sn") == camera.sn }
                                    WallCameraState(camera.sn, camera.name, item.optString("mode", camera.mode), states[camera.sn] ?: "idle", item.optBoolean("still"))
                                })
                                refreshWall()
                            }
                            "motion" -> {
                                val sn = event.optString("sn")
                                wallPolicy.onMotion(sn, SystemClock.elapsedRealtime(), event.optBoolean("still"))
                                snapshotVersions[sn] = (snapshotVersions[sn] ?: 0L) + 1
                                refreshWall()
                            }
                            "streamState" -> {
                                val sn = event.optString("sn")
                                states[sn] = event.optString("state", "idle")
                                if (event.has("nativeRendition")) cameras = cameras.map { camera ->
                                    if (camera.sn != sn) camera else {
                                        val updated = camera.copy(nativeRendition = parseNativeRendition(event.optJSONObject("nativeRendition")))
                                        if (updated != camera) releaseChangedPlayback(updated, "stream-metadata-changed")
                                        updated
                                    }
                                }
                                wallPolicy.onStreamState(sn, states[sn] ?: "idle")
                                refreshWall()
                            }
                        }
                    }
                } catch (e: Exception) { android.util.Log.e("EufyWallTV", "Invalid event", e) }
            }
            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                ui.post { controlRecovery.lost(wallVisible && socket === webSocket,
                    "failure:${t.javaClass.simpleName}") }
            }
            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                ui.post { controlRecovery.lost(wallVisible && socket === webSocket, "closed:$code") }
            }
        })
    }

    private fun hold(sn: String, method: String) {
        if (bridge.isBlank()) return
        val url = "$bridge/hold/$sn?owner=$owner"
        holds.setDesired(url, method == "POST")
    }

    private fun startDiscovery() {
        cancelCameraLoad()
        stopDiscovery()
        status?.text = "Finding your bridge…"
        val wifi = applicationContext.getSystemService(WIFI_SERVICE) as? WifiManager
        multicast = wifi?.createMulticastLock("eufy-wall-discovery")?.apply { setReferenceCounted(false); acquire() }
        nsd = getSystemService(NSD_SERVICE) as NsdManager
        discovery = object : NsdManager.DiscoveryListener {
            override fun onDiscoveryStarted(type: String) {}
            override fun onStartDiscoveryFailed(type: String, code: Int) { ui.post { status?.text = "Discovery error $code • enter IP manually"; stopDiscovery() } }
            override fun onStopDiscoveryFailed(type: String, code: Int) {}
            override fun onDiscoveryStopped(type: String) {}
            override fun onServiceLost(info: NsdServiceInfo) {}
            override fun onServiceFound(info: NsdServiceInfo) {
                if (info.serviceType.trimEnd('.') != "_eufy-wall._tcp") return
                nsd?.resolveService(info, object : NsdManager.ResolveListener {
                    override fun onResolveFailed(service: NsdServiceInfo, code: Int) { ui.post { status?.text = "Discovery resolve error $code • enter IP manually" } }
                    override fun onServiceResolved(service: NsdServiceInfo) {
                        val host: InetAddress = service.host ?: return
                        if (host.isLoopbackAddress || host.isAnyLocalAddress) {
                            ui.post { status?.text = "Ignoring bridge advertised on localhost. Searching for a LAN address…" }
                            return
                        }
                        val port = service.port
                        val rtspPort = service.attributes["rtsp"]?.toString(Charsets.UTF_8)?.toIntOrNull()
                        if (port != 3000 || rtspPort == null || rtspPort !in 1..65535) return
                        val address = host.hostAddress ?: return
                        val uriHost = if (address.contains(':')) "[${address.replace("%", "%25")}]" else address
                        ui.post { stopDiscovery(); loadCameras("http://$uriHost:$port", false) }
                    }
                })
            }
        }
        try { nsd?.discoverServices("_eufy-wall._tcp", NsdManager.PROTOCOL_DNS_SD, discovery) }
        catch (e: Exception) { status?.text = "Discovery unavailable: ${e.message}"; stopDiscovery() }
        val active = discovery
        ui.postDelayed({ if (discovery === active && active != null) { status?.text = "No bridge found. Enter its IP or try discovery again"; stopDiscovery() } }, 10_000)
    }

    private fun stopDiscovery() {
        discovery?.let { try { nsd?.stopServiceDiscovery(it) } catch (_: Exception) {} }
        discovery = null
        multicast?.let { if (it.isHeld) it.release() }; multicast = null
    }

    private fun stopWall() {
        ui.removeCallbacks(reconnect); ui.removeCallbacks(holdRefresh); ui.removeCallbacks(wallTick)
        socket?.cancel(); socket = null
        players.keys.toList().forEach { release(it, "wall-stopped") }
        images.keys.toList().forEach { snapshots.cancel(it) }
        labels.clear(); surfaces.clear(); images.clear(); resolvedTiles.clear(); snapshotKeys.clear()
        held.toList().forEach { hold(it, "DELETE") }; held.clear()
        holds.releaseAll()
        wallPolicy = WallPolicy()
    }

    @Deprecated("Deprecated in Java")
    override fun onBackPressed() { cancelCameraLoad(); if (wallVisible) showSetup() else if (editingWall || (connectionSettings && cameras.isNotEmpty())) showSetup() else super.onBackPressed() }
    override fun onStart() {
        super.onStart()
        activityStarted = true
        if (resumeWallOnStart) { resumeWallOnStart = false; showWall() }
        else if (!cameraLoadInFlight && !wallVisible) savedWallBridge?.let { loadCameras(it, true) }
    }
    override fun onStop() {
        activityStarted = false
        cancelCameraLoad(clearSavedWall = false)
        if (wallVisible) {
            resumeWallOnStart = true
            wallVisible = false
            stopWall()
        }
        stopDiscovery()
        super.onStop()
    }
    override fun onDestroy() { destroyed = true; cancelCameraLoad(); stopDiscovery(); wallVisible = false; stopWall(); holds.close(); snapshots.close(); work.shutdown(); super.onDestroy() }
}
