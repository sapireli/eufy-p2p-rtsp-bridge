package com.eufywall.tv

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
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.DefaultRenderersFactory
import androidx.media3.exoplayer.DefaultLoadControl
import androidx.media3.exoplayer.rtsp.RtspMediaSource
import androidx.media3.exoplayer.video.VideoFrameMetadataListener
import androidx.media3.ui.PlayerView
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
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

data class Camera(val sn: String, val name: String, val rtsp: String, val mode: String, val streaming: Boolean,
                  val dual: Boolean, val dualView: String?, val width: Int, val height: Int)

class MainActivity : Activity() {
    private val ui = Handler(Looper.getMainLooper())
    private val work = Executors.newSingleThreadExecutor()
    private val http = OkHttpClient.Builder().connectTimeout(4, TimeUnit.SECONDS).readTimeout(5, TimeUnit.SECONDS).build()
    private val prefs by lazy { getSharedPreferences("wall", MODE_PRIVATE) }
    private val owner by lazy { prefs.getString("owner", null) ?: UUID.randomUUID().toString().also { prefs.edit().putString("owner", it).apply() } }
    private var bridge = ""
    private var cameras = listOf<Camera>()
    private val chosen = linkedSetOf<String>()
    private val states = mutableMapOf<String, String>()
    private val players = mutableMapOf<String, ExoPlayer>()
    private val frameWatchdogs = mutableMapOf<String, Runnable>()
    private val playbackControls = mutableMapOf<String, Runnable>()
    private val labels = mutableMapOf<String, TextView>()
    private val surfaces = mutableMapOf<String, PlayerView>()
    private var socket: WebSocket? = null
    private var wallVisible = false
    private var activityStarted = false
    private var resumeWallOnStart = false
    private var nsd: NsdManager? = null
    private var discovery: NsdManager.DiscoveryListener? = null
    private var multicast: WifiManager.MulticastLock? = null
    private var status: TextView? = null
    private var ipEntry: EditText? = null
    private var decoderFailures = 0
    private var decoderRecoveryScheduled = false
    private val reconnect = Runnable { if (wallVisible) connectEvents() }
    private val firstFrameTimeoutMs = 15_000L
    private val liveFrameTimeoutMs = 5_000L
    private val holdRefresh = object : Runnable {
        override fun run() {
            if (!wallVisible) return
            chosen.forEach { hold(it, "POST") }
            ui.postDelayed(this, 20_000)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.decorView.systemUiVisibility = View.SYSTEM_UI_FLAG_FULLSCREEN or View.SYSTEM_UI_FLAG_HIDE_NAVIGATION or View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
        bridge = prefs.getString("bridge", "") ?: ""
        chosen.addAll((prefs.getString("chosen", "") ?: "").split(',').filter { it.isNotBlank() }.take(4))
        showSetup()
        if (bridge.isNotBlank()) loadCameras(bridge, true) else startDiscovery()
    }

    private fun text(value: String, size: Float = 22f): TextView = TextView(this).apply {
        this.text = value; textSize = size; setTextColor(0xffffffff.toInt()); setPadding(16, 10, 16, 10)
    }

    private fun button(value: String, action: () -> Unit): Button = Button(this).apply {
        text = value; textSize = 20f; setOnClickListener { action() }
    }

    private fun showSetup(message: String = "Enter the bridge IP or use discovery") {
        resumeWallOnStart = false
        wallVisible = false
        stopWall()
        val root = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(40, 24, 40, 24); setBackgroundColor(0xff101c27.toInt()) }
        root.addView(text("Eufy Wall", 32f))
        status = text(message, 18f).also { root.addView(it) }
        ipEntry = EditText(this).apply {
            setSingleLine(true); hint = "Bridge IP, e.g. 192.168.1.10"; setText(bridge.removePrefix("http://").substringBefore(':'))
            setTextColor(0xffffffff.toInt()); setHintTextColor(0xffaabbcc.toInt()); inputType = android.text.InputType.TYPE_CLASS_TEXT
        }.also { root.addView(it) }
        val actions = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
        actions.addView(button("Connect") {
            val value = ipEntry?.text.toString().trim()
            if (value.isNotEmpty()) try { loadCameras(normalize(value), false) }
            catch (e: Exception) { status?.text = "Invalid bridge address: ${e.message}" }
        })
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
                    .map { Camera(it.getString("sn"), it.optString("name", it.getString("sn")), it.getString("rtsp"), it.optString("mode", "always"), it.optBoolean("streaming"), it.optBoolean("dual"), if (it.isNull("dualView")) null else it.optString("dualView").ifBlank { null }, it.optInt("width"), it.optInt("height")) }
                ui.post {
                    bridge = url; cameras = list; prefs.edit().putString("bridge", url).apply()
                    states.clear(); list.forEach { states[it.sn] = if (it.streaming) "live" else "idle" }
                    if (autoOpen && chosen.any { sn -> list.any { it.sn == sn } }) showWall()
                    else showSetup("Connected to $url • ${list.size} camera(s)")
                }
            } catch (e: Exception) {
                ui.post { status?.text = "Bridge unavailable: ${e.message}"; if (wallVisible) ui.postDelayed(reconnect, 5_000) }
            }
        }
    }

    private fun showCameras(root: LinearLayout) {
        root.addView(text("Choose up to four cameras", 22f))
        cameras.forEach { cam ->
            val b = button(chooseLabel(cam)) {
                if (!chosen.remove(cam.sn)) {
                    if (chosen.size >= 4) { status?.text = "Four cameras maximum"; return@button }
                    chosen.add(cam.sn)
                }
                prefs.edit().putString("chosen", chosen.joinToString(",")).apply()
                (itForCamera(root, cam.sn))?.text = chooseLabel(cam)
            }
            b.tag = cam.sn; root.addView(b)
            if (cam.dual) {
                val viewButton = button(viewLabel(cam)) { setDualView(cam.sn, itForView(root, cam.sn)) }
                viewButton.tag = "view:${cam.sn}"
                root.addView(viewButton)
            }
        }
        root.addView(button("Start live wall") { if (chosen.isEmpty()) status?.text = "Choose a camera first" else showWall() })
    }

    private fun itForCamera(root: LinearLayout, sn: String): Button? = (0 until root.childCount).mapNotNull { root.getChildAt(it) as? Button }.firstOrNull { it.tag == sn }
    private fun itForView(root: LinearLayout, sn: String): Button? = (0 until root.childCount).mapNotNull { root.getChildAt(it) as? Button }.firstOrNull { it.tag == "view:$sn" }
    private fun chooseLabel(cam: Camera) = (if (chosen.contains(cam.sn)) "✓ " else "○ ") + cam.name
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
        if (bridge.isBlank()) return
        stopDiscovery()
        wallVisible = true
        val selected = chosen.mapNotNull { sn -> cameras.firstOrNull { it.sn == sn } }.take(4)
        if (selected.isEmpty()) { showSetup("Selected cameras unavailable"); return }
        fun tileFor(cam: Camera): FrameLayout {
            val tile = FrameLayout(this).apply { setBackgroundColor(0xff000000.toInt()) }
            val videoView = layoutInflater.inflate(R.layout.video_tile, tile, false) as PlayerView
            videoView.keepScreenOn = true
            tile.addView(videoView)
            val label = text("${cam.name} • waiting", 18f).apply { setBackgroundColor(0x99000000.toInt()) }
            tile.addView(label, FrameLayout.LayoutParams(-1, -2, Gravity.BOTTOM))
            labels[cam.sn] = label; surfaces[cam.sn] = videoView
            updateTile(cam.sn)
            return tile
        }
        fun isPortrait(cam: Camera): Boolean {
            // Geometry from the live bridge stream is authoritative. The selected camera view is a
            // fallback while a newly switched stream is still reporting its previous dimensions.
            if (cam.dualView?.startsWith("pip-") == true || cam.dualView == "single") return false
            if (cam.width > 0 && cam.height > 0) return cam.height > cam.width
            return cam.dualView == "split"
        }
        val portraits = selected.filter(::isPortrait)
        val landscapes = selected.filterNot(::isPortrait)
        val portraitPair = portraits.size == 2 && landscapes.isNotEmpty() && selected.size in 3..4
        val wall: ViewGroup = if (portraitPair) {
            // Two portrait views use the full height on the left. Balcony occupies the upper-right
            // 16:9 tile; the lower-right tile stays empty until a fourth camera is selected.
            val root = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL; setBackgroundColor(0xff000000.toInt()) }
            val left = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
            val right = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
            root.addView(left, LinearLayout.LayoutParams(0, -1, 7f))
            root.addView(right, LinearLayout.LayoutParams(0, -1, 3f))
            portraits.forEach { cam ->
                left.addView(tileFor(cam), LinearLayout.LayoutParams(0, -1, 1f).apply { setMargins(2, 2, 2, 2) })
            }
            right.addView(tileFor(landscapes[0]), LinearLayout.LayoutParams(-1, 0, 1f).apply { setMargins(2, 2, 2, 2) })
            val fourth = landscapes.getOrNull(1)
            right.addView(fourth?.let(::tileFor) ?: FrameLayout(this).apply { setBackgroundColor(0xff000000.toInt()) },
                LinearLayout.LayoutParams(-1, 0, 1f).apply { setMargins(2, 2, 2, 2) })
            root
        } else if (selected.size == 3 && portraits.size == 1 && landscapes.size == 2) {
            // A camera in PiP emits landscape video. Keep the remaining split view tall and give
            // Balcony and PiP landscape tiles on the right.
            val root = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL; setBackgroundColor(0xff000000.toInt()) }
            root.addView(tileFor(portraits[0]), LinearLayout.LayoutParams(0, -1, 4f).apply { setMargins(2, 2, 2, 2) })
            val right = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
            root.addView(right, LinearLayout.LayoutParams(0, -1, 6f))
            landscapes.forEach { cam ->
                right.addView(tileFor(cam), LinearLayout.LayoutParams(-1, 0, 1f).apply { setMargins(2, 2, 2, 2) })
            }
            root
        } else {
            GridLayout(this).apply {
                rowCount = if (selected.size <= 2) 1 else 2
                columnCount = if (selected.size == 1) 1 else 2
                setBackgroundColor(0xff000000.toInt())
                selected.forEachIndexed { index, cam ->
                    val params = GridLayout.LayoutParams(GridLayout.spec(if (selected.size <= 2) 0 else index / 2, 1f), GridLayout.spec(if (selected.size == 1) 0 else index % 2, 1f)).apply { width = 0; height = 0; setMargins(2, 2, 2, 2) }
                    addView(tileFor(cam), params)
                }
            }
        }
        setContentView(wall)
        chosen.forEach { hold(it, "POST") }
        ui.postDelayed(holdRefresh, 20_000)
        connectEvents()
    }

    private fun updateTile(sn: String) {
        if (decoderRecoveryScheduled) return
        val cam = cameras.firstOrNull { it.sn == sn } ?: return
        val state = states[sn] ?: "idle"
        val label = labels[sn] ?: return
        if (state == "live") {
            if (players.containsKey(sn)) return
            label.text = "${cam.name} • connecting"
            val renderers = DefaultRenderersFactory(this)
                .setEnableDecoderFallback(true)
            // These are live RTSP cameras: the default 1s start / 2s rebuffer waits
            // add a persistent delay. Keep a small jitter allowance instead.
            val loadControl = DefaultLoadControl.Builder()
                .setBufferDurationsMs(500, 1500, 200, 500)
                .build()
            val player = ExoPlayer.Builder(this, renderers).setLoadControl(loadControl).build()
            val startedAt = SystemClock.elapsedRealtime()
            val firstFrameRendered = AtomicBoolean(false)
            val lastFrameAt = AtomicLong(startedAt)
            player.setVideoFrameMetadataListener(VideoFrameMetadataListener { _, _, _, _ ->
                if (firstFrameRendered.get()) lastFrameAt.set(SystemClock.elapsedRealtime())
            })
            players[sn] = player
            surfaces[sn]?.player = player
            val playbackControl = LiveRtspPlaybackControl()
            var lastPlaybackLogAt = 0L
            val control = object : Runnable {
                override fun run() {
                    if (!wallVisible || states[sn] != "live" || players[sn] !== player) return
                    val bufferedMs = player.totalBufferedDuration
                    val speed = playbackControl.speedFor(bufferedMs, player.isPlaying)
                    val changed = player.playbackParameters.speed != speed
                    if (changed) player.setPlaybackSpeed(speed)
                    val now = SystemClock.elapsedRealtime()
                    if (changed || now - lastPlaybackLogAt >= 5_000) {
                        android.util.Log.i("EufyWallTV", "Live playback ${cam.sn}: bufferedMs=$bufferedMs speed=$speed positionMs=${player.currentPosition} playing=${player.isPlaying}")
                        lastPlaybackLogAt = now
                    }
                    ui.postDelayed(this, 500)
                }
            }
            playbackControls[sn] = control
            ui.postDelayed(control, 500)
            val watchdog = object : Runnable {
                override fun run() {
                    if (!wallVisible || states[sn] != "live" || players[sn] !== player) return
                    val rendered = firstFrameRendered.get()
                    val silentForMs = SystemClock.elapsedRealtime() - (if (rendered) lastFrameAt.get() else startedAt)
                    if (silentForMs >= (if (rendered) liveFrameTimeoutMs else firstFrameTimeoutMs)) {
                        label.text = "${cam.name} • video stalled, reconnecting"
                        android.util.Log.w("EufyWallTV", "No video frames for ${silentForMs}ms: ${cam.sn}; restarting RTSP player")
                        release(sn)
                        updateTile(sn)
                    } else {
                        ui.postDelayed(this, 1_000)
                    }
                }
            }
            frameWatchdogs[sn] = watchdog
            ui.postDelayed(watchdog, 1_000)
            player.addListener(object : Player.Listener {
                override fun onPlaybackStateChanged(playbackState: Int) {
                    if (players[sn] !== player) return
                    if (playbackState == Player.STATE_READY) label.text = cam.name
                    android.util.Log.i("EufyWallTV", "Playback ${cam.sn}: state=$playbackState bufferedMs=${player.totalBufferedDuration} elapsedMs=${SystemClock.elapsedRealtime() - startedAt}")
                }
                override fun onRenderedFirstFrame() {
                    if (players[sn] !== player) return
                    lastFrameAt.set(SystemClock.elapsedRealtime())
                    firstFrameRendered.set(true)
                    decoderFailures = 0
                    android.util.Log.i("EufyWallTV", "Rendered first frame for ${cam.sn} after ${SystemClock.elapsedRealtime() - startedAt}ms; bufferedMs=${player.totalBufferedDuration}")
                }
                override fun onPlayerError(error: PlaybackException) {
                    if (players[sn] !== player) return
                    label.text = "${cam.name} • ${error.errorCodeName}"
                    android.util.Log.e("EufyWallTV", "RTSP ${cam.sn}: ${error.errorCodeName}", error)
                    if (error.errorCode == PlaybackException.ERROR_CODE_DECODER_INIT_FAILED) {
                        decoderFailures++
                        if (decoderFailures >= 2) {
                            recoverDecoder()
                            return
                        }
                    }
                    ui.postDelayed({ if (wallVisible && states[sn] == "live" && players[sn] === player) { release(sn); updateTile(sn) } }, 5_000)
                }
            })
            player.setMediaSource(RtspMediaSource.Factory().setForceUseRtpTcp(true).createMediaSource(MediaItem.fromUri(cam.rtsp)))
            player.prepare(); player.playWhenReady = true
        } else {
            release(sn)
            label.text = "${cam.name} • ${if (state == "starting") "starting" else "idle"}"
        }
    }

    private fun release(sn: String) {
        playbackControls.remove(sn)?.let { ui.removeCallbacks(it) }
        frameWatchdogs.remove(sn)?.let { ui.removeCallbacks(it) }
        surfaces[sn]?.player = null
        players.remove(sn)?.release()
    }

    private fun recoverDecoder() {
        if (decoderRecoveryScheduled) return
        decoderRecoveryScheduled = true
        players.keys.toList().forEach { release(it) }
        val last = prefs.getLong("decoder_restart_at", 0)
        val now = System.currentTimeMillis()
        if (now - last < 5 * 60_000) {
            labels.values.forEach { it.text = "Hardware decoder busy • retrying" }
            android.util.Log.e("EufyWallTV", "Decoder recovery already attempted recently")
            ui.postDelayed({
                if (wallVisible) {
                    decoderRecoveryScheduled = false
                    decoderFailures = 0
                    chosen.forEach { updateTile(it) }
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
                                for (i in 0 until arr.length()) { val item = arr.getJSONObject(i); states[item.getString("sn")] = item.optString("state", "idle") }
                                chosen.forEach { updateTile(it) }
                            }
                            "streamState" -> { val sn = event.optString("sn"); states[sn] = event.optString("state", "idle"); updateTile(sn) }
                        }
                    }
                } catch (e: Exception) { android.util.Log.e("EufyWallTV", "Invalid event", e) }
            }
            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) { ui.post { if (wallVisible && socket === webSocket) { players.keys.toList().forEach { release(it) }; labels.values.forEach { it.text = "Bridge disconnected • retrying" }; ui.postDelayed(reconnect, 5_000) } } }
            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) { ui.post { if (wallVisible && socket === webSocket) { players.keys.toList().forEach { release(it) }; labels.values.forEach { it.text = "Bridge disconnected • retrying" }; ui.postDelayed(reconnect, 5_000) } } }
        })
    }

    private fun hold(sn: String, method: String) {
        if (bridge.isBlank()) return
        val url = "$bridge/hold/$sn?owner=$owner"
        work.execute { try { http.newCall(Request.Builder().url(url).method(method, if (method == "POST") ByteArray(0).toRequestBody(null) else null).build()).execute().close() } catch (_: Exception) {} }
    }

    private fun startDiscovery() {
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
        ui.removeCallbacks(reconnect); ui.removeCallbacks(holdRefresh)
        socket?.cancel(); socket = null
        players.keys.toList().forEach { release(it) }
        labels.clear(); surfaces.clear()
        if (bridge.isNotBlank()) chosen.forEach { hold(it, "DELETE") }
    }

    @Deprecated("Deprecated in Java")
    override fun onBackPressed() { if (wallVisible) showSetup("Choose cameras or change bridge") else super.onBackPressed() }
    override fun onStart() {
        super.onStart()
        activityStarted = true
        if (resumeWallOnStart) { resumeWallOnStart = false; showWall() }
    }
    override fun onStop() {
        activityStarted = false
        if (wallVisible) {
            resumeWallOnStart = true
            wallVisible = false
            stopWall()
        }
        stopDiscovery()
        super.onStop()
    }
    override fun onDestroy() { stopDiscovery(); wallVisible = false; stopWall(); work.shutdown(); super.onDestroy() }
}
