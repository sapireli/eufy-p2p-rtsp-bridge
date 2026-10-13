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
    private val nativeFallback = NativeFallback()
    private var nativeProfileProbeUntil = 0L
    private val frameWatchdogs = mutableMapOf<String, Runnable>()
    private val labels = mutableMapOf<String, TextView>()
    private val surfaces = mutableMapOf<String, FrameLayout>()
    private var socket: WebSocket? = null
    private var wallVisible = false
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
        if (errors.isNotEmpty()) { Toast.makeText(this, errors.first(), Toast.LENGTH_LONG).show(); return false }
        wallConfig = next
        chosen.clear(); chosen.addAll(next.tiles.filterNot { it.spec.motion }.map { it.spec.camera })
        prefs.edit().putString("configuration", next.encode()).putString("chosen", chosen.joinToString(",")).apply()
        return true
    }

    private fun showSetup(message: String = "Enter the bridge hostname or IP, or use discovery") {
        cancelCameraLoad()
        resumeWallOnStart = false
        wallVisible = false
        stopWall()
        val root = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(40, 24, 40, 24); setBackgroundColor(0xff101c27.toInt()) }
        root.addView(text("Eufy Wall", 34f))
        root.addView(text("Live cameras · motion views · battery-friendly snapshots", 17f))
        status = text(message, 18f).also { root.addView(it) }
        ipEntry = EditText(this).apply {
            setSingleLine(true); hint = "Bridge hostname or IP, e.g. homebridge.lan"; setText(bridge.removePrefix("http://").substringBefore(':'))
            setTextColor(0xffffffff.toInt()); setHintTextColor(0xffaabbcc.toInt()); inputType = android.text.InputType.TYPE_CLASS_TEXT
        }.also { root.addView(it) }
        val actions = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
        val connectButton = button("Connect") {
            cancelCameraLoad()
            val value = ipEntry?.text.toString().trim()
            if (value.isNotEmpty()) try { loadCameras(normalize(value), false) }
            catch (e: Exception) { status?.text = "Invalid bridge address: ${e.message}" }
        }.apply { id = View.generateViewId() }
        actions.addView(connectButton)
        ipEntry?.nextFocusDownId = connectButton.id
        ipEntry?.setOnKeyListener { _, keyCode, event ->
            if (keyCode == android.view.KeyEvent.KEYCODE_DPAD_DOWN) {
                if (event.action == android.view.KeyEvent.ACTION_DOWN) connectButton.requestFocus()
                true
            } else false
        }
        actions.addView(button("Discover") { startDiscovery() })
        root.addView(actions)
        val scroller = android.widget.ScrollView(this).apply { addView(root) }
        setContentView(scroller)
        if (cameras.isNotEmpty()) showCameras(root)
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
                    else showSetup("Connected to $url • ${list.size} camera(s)")
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

    private fun showCameras(root: LinearLayout) {
        val actions = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
        val startButton = button("▶ Start wall") {
            if (wallConfig.tiles.isEmpty()) status?.text = "Add a camera or a motion tile first" else showWall()
        }
        actions.addView(startButton)
        ui.post { if (!wallVisible && activityStarted && !destroyed) startButton.requestFocus() }
        actions.addView(button(if (wallConfig.layout == WallLayoutMode.AUTO) "Layout: Automatic" else "Layout: ${wallConfig.columns} × ${wallConfig.rows}") { editLayout() })
        actions.addView(button("+ Motion tile") {
            val tile = ConfiguredWallTile(WallTileSpec(UUID.randomUUID().toString(), motion = true))
            if (saveConfiguration(wallConfig.copy(tiles = wallConfig.tiles + tile))) showSetup("Motion tile added · choose its watched cameras")
        })
        root.addView(actions)
        root.addView(text("Your wall · ${wallConfig.tiles.size} tile(s)", 24f))
        root.addView(text("Select a tile to change its source, size or motion behavior. On the live wall, OK opens camera actions; Back opens setup.", 16f))
        wallConfig.tiles.forEachIndexed { index, tile ->
            val spec = tile.spec
            val name = if (spec.motion) "Latest motion" else cameras.firstOrNull { it.sn == spec.camera }?.name ?: spec.camera
            val detail = if (spec.motion) "${if (spec.watch.isEmpty()) "All cameras" else "${spec.watch.size} watched"} · dwell ${spec.dwellMs / 1000}s · ${if (spec.blankAfterMs == 0L) "keep visible" else "blank after ${spec.blankAfterMs / 1000}s"}" else "${if (cameras.firstOrNull { it.sn == spec.camera }?.mode == "always") "Always on" else "Passive battery viewing"} · ${tile.columnSpan} × ${tile.rowSpan} cells"
            val card = button("${index + 1}.  $name\n$detail") { editTile(tile) }
            card.gravity = Gravity.START or Gravity.CENTER_VERTICAL
            root.addView(card, LinearLayout.LayoutParams(-1, -2).apply { setMargins(0, 6, 0, 6) })
        }
        root.addView(text("Add cameras", 24f))
        cameras.forEach { cam ->
            val row = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
            row.addView(button("+ ${cam.name}  ·  ${if (cam.mode == "always") "Always on" else "Battery / on demand"}") {
                val tile = ConfiguredWallTile(WallTileSpec(UUID.randomUUID().toString(), camera = cam.sn))
                if (saveConfiguration(wallConfig.copy(tiles = wallConfig.tiles + tile))) showSetup("${cam.name} added")
            }, LinearLayout.LayoutParams(0, -2, 1f))
            if (cam.dual) row.addView(button(viewLabel(cam)) { setDualView(cam.sn, null) })
            root.addView(row, LinearLayout.LayoutParams(-1, -2).apply { setMargins(0, 5, 0, 5) })
        }
        root.addView(text("Fixed battery cameras stay asleep until motion or an explicit timed wake. More live tiles require enough decoder capacity; hardware remains first, software is the fallback.", 16f))
    }

    private fun editLayout() {
        var layout = wallConfig.layout; var columns = wallConfig.columns; var rows = wallConfig.rows
        val body = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(28, 16, 28, 16) }
        val modeButton = button("Mode: $layout") {}
        modeButton.setOnClickListener { layout = if (layout == WallLayoutMode.AUTO) WallLayoutMode.GRID else WallLayoutMode.AUTO; modeButton.text = "Mode: $layout" }
        body.addView(modeButton)
        val colButton = button("Columns: $columns") {}; colButton.setOnClickListener { columns = columns % 6 + 1; colButton.text = "Columns: $columns" }; body.addView(colButton)
        val rowButton = button("Rows: $rows") {}; rowButton.setOnClickListener { rows = rows % 6 + 1; rowButton.text = "Rows: $rows" }; body.addView(rowButton)
        body.addView(text("Automatic preserves portrait layouts and fits up to 36 tiles. Custom grids use the tile sizes and order you choose.", 16f))
        val dialog = AlertDialog.Builder(this).setTitle("Wall layout").setView(body).setNegativeButton("Cancel", null).setPositiveButton("Save", null).create()
        dialog.setOnShowListener { dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
            if (saveConfiguration(wallConfig.copy(layout = layout, columns = columns, rows = rows))) { dialog.dismiss(); showSetup("Layout saved") }
        } }; dialog.show()
    }

    private fun editTile(tile: ConfiguredWallTile) {
        var spec = tile.spec; var colSpan = tile.columnSpan; var rowSpan = tile.rowSpan
        val body = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(24, 12, 24, 12) }
        fun sourceName() = if (spec.motion) "Latest motion" else cameras.firstOrNull { it.sn == spec.camera }?.name ?: spec.camera
        val source = button("Source: ${sourceName()}") {}
        source.setOnClickListener {
            AlertDialog.Builder(this).setTitle("Tile source").setItems((listOf("Latest motion") + cameras.map { it.name }).toTypedArray()) { _, which ->
                spec = if (which == 0) spec.copy(camera = "", motion = true) else spec.copy(camera = cameras[which - 1].sn, motion = false)
                source.text = "Source: ${sourceName()}"
            }.show()
        }; body.addView(source)
        val watched = button(if (spec.watch.isEmpty()) "Watch: All cameras" else "Watch: ${spec.watch.size} cameras") {}
        watched.setOnClickListener {
            val selection = spec.watch.toMutableSet()
            AlertDialog.Builder(this).setTitle("Watched cameras · none means all")
                .setMultiChoiceItems(cameras.map { it.name }.toTypedArray(), cameras.map { it.sn in selection }.toBooleanArray()) { _, index, checked ->
                    if (checked) selection.add(cameras[index].sn) else selection.remove(cameras[index].sn)
                }.setPositiveButton("Done") { _, _ -> spec = spec.copy(watch = selection.toList()); watched.text = if (selection.isEmpty()) "Watch: All cameras" else "Watch: ${selection.size} cameras" }.show()
        }; body.addView(watched)
        fun timingButton(name: String, values: List<Long>, initial: Long, update: (Long) -> Unit) {
            var value = initial
            val b = button("$name: ${if (value == 0L) "Off" else "${value / 1000}s"}") {}
            b.setOnClickListener { value = values[(values.indexOf(value) + 1).mod(values.size)]; update(value); b.text = "$name: ${if (value == 0L) "Off" else "${value / 1000}s"}" }; body.addView(b)
        }
        timingButton("Minimum dwell", listOf(0, 5000, 10000, 15000, 30000), spec.dwellMs) { spec = spec.copy(dwellMs = it) }
        timingButton("Blank after no motion", listOf(0, 15000, 30000, 60000, 120000), spec.blankAfterMs) { spec = spec.copy(blankAfterMs = it) }
        val size = button("Size: $colSpan × $rowSpan cells") {}
        size.setOnClickListener { colSpan = colSpan % wallConfig.columns + 1; size.text = "Size: $colSpan × $rowSpan cells" }; body.addView(size)
        val height = button("Height: $rowSpan cells") {}
        height.setOnClickListener { rowSpan = rowSpan % wallConfig.rows + 1; height.text = "Height: $rowSpan cells"; size.text = "Size: $colSpan × $rowSpan cells" }; body.addView(height)
        if (wallConfig.layout == WallLayoutMode.AUTO) {
            size.isEnabled = false; height.isEnabled = false
            body.addView(text("Choose a custom grid in Layout to resize tiles.", 16f))
        }
        val move = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
        fun moveTile(delta: Int): Boolean {
            val list = wallConfig.tiles.toMutableList(); val index = list.indexOfFirst { it.spec.id == tile.spec.id }; val to = index + delta
            if (index < 0 || to !in list.indices) return false
            list[index] = tile.copy(spec = spec, columnSpan = colSpan, rowSpan = rowSpan)
            val moving = list.removeAt(index); list.add(to, moving)
            return saveConfiguration(wallConfig.copy(tiles = list))
        }
        val dialog = AlertDialog.Builder(this).setTitle("Edit tile").setView(android.widget.ScrollView(this).apply { addView(body) }).setNegativeButton("Cancel", null).setPositiveButton("Save", null).create()
        move.addView(button("Move earlier") { if (moveTile(-1)) { dialog.dismiss(); showSetup("Tile order saved") } }.apply { isEnabled = wallConfig.tiles.firstOrNull()?.spec?.id != tile.spec.id })
        move.addView(button("Move later") { if (moveTile(1)) { dialog.dismiss(); showSetup("Tile order saved") } }.apply { isEnabled = wallConfig.tiles.lastOrNull()?.spec?.id != tile.spec.id }); body.addView(move)
        body.addView(button("Remove tile") { dialog.dismiss(); if (saveConfiguration(wallConfig.copy(tiles = wallConfig.tiles.filterNot { it.spec.id == tile.spec.id }))) showSetup("Tile removed") })
        dialog.setOnShowListener { dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
            val updated = tile.copy(spec = spec, columnSpan = colSpan, rowSpan = rowSpan)
            if (saveConfiguration(wallConfig.copy(tiles = wallConfig.tiles.map { if (it.spec.id == tile.spec.id) updated else it }))) { dialog.dismiss(); showSetup("Tile saved") }
        } }; dialog.show()
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
                    status?.text = "${cam.name}: ${if (next == "split") "Split" else "PiP"} selected. Start live wall to see it."
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
            if (options[which] == "Edit wall") showSetup()
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
            val native = cam.nativeRendition?.takeIf { nativeFallback.permits(cam.sn) }
            val hardware = native?.let { nativeHardwareDecoder(it, BuildConfig.DEBUG && SystemClock.elapsedRealtime() < nativeProfileProbeUntil) }
            val playback = if (native != null && hardware != null) cam.copy(rtsp = native.rtsp,
                width = native.width, height = native.height, freshness = native.freshness,
                nativeDecoderName = hardware, rtspTcpPacketSize = null) else cam
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

    private fun release(sn: String, reason: String) {
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
                                                resolvedTiles.filterValues { it.camera == sn }.keys.toList().forEach { release(it, "hello-metadata-changed") }
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
                                        if (updated != camera) resolvedTiles.filterValues { it.camera == sn }.keys.toList().forEach { release(it, "stream-metadata-changed") }
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
        status?.text = "Searching for _eufy-wall._tcp…"
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
    override fun onBackPressed() { cancelCameraLoad(); if (wallVisible) showSetup("Choose cameras or change bridge") else super.onBackPressed() }
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
