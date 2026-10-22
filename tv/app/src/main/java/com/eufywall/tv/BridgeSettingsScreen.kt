package com.eufywall.tv

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.os.Handler
import android.os.Looper
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.Toast
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.util.concurrent.Executors

/** Shared bridge settings. Display preferences remain in the wall editor. */
internal class BridgeSettingsScreen(
    private val activity: Activity,
    private val http: OkHttpClient,
    private val bridge: () -> String,
    private val cameras: () -> List<Camera>,
    private val page: (String, String) -> LinearLayout,
    private val button: (String, () -> Unit) -> android.widget.Button,
    private val back: () -> Unit,
) {
    private val executor = Executors.newSingleThreadExecutor()
    private val ui = Handler(Looper.getMainLooper())
    private var generation = 0
    var visible = false
        private set
    private fun tokenFile(): java.io.File {
        val key = java.security.MessageDigest.getInstance("SHA-256").digest(bridge().toByteArray(Charsets.UTF_8))
            .joinToString("") { "%02x".format(it.toInt() and 255) }
        return java.io.File(activity.noBackupFilesDir, "bridge-admin-$key")
    }
    private fun administratorToken(): String? = runCatching { tokenFile().readText().trim() }.getOrNull()
    private var snapshot = JSONObject()
    private var pendingExport: String? = null

    fun leave() { visible = false; generation++ }
    fun close() { leave(); executor.shutdownNow() }

    fun authorize(builder: Request.Builder): Request.Builder {
        administratorToken()?.takeIf { it.isNotBlank() }?.let { builder.header("Authorization", "Bearer $it") }
        return builder
    }

    private fun request(path: String, method: String = "GET", payload: JSONObject? = null): JSONObject {
        val builder = Request.Builder().url("${bridge()}$path")
        authorize(builder)
        if (payload != null) builder.method(method, payload.toString().toRequestBody("application/json".toMediaType()))
        return http.newCall(builder.build()).execute().use { response ->
            val body = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                val detail = runCatching { JSONObject(body).optString("error") }.getOrDefault("")
                if (response.code == 401 || response.code == 403) error("Enter the bridge administrator token to manage settings.")
                if (response.code == 409) error("Settings changed on another client. Reload before trying again.")
                if (response.code == 404) error("This bridge needs an update to support shared settings.")
                error(detail.ifBlank { "Bridge returned HTTP ${response.code}" })
            }
            JSONObject(body)
        }
    }

    private fun run(action: () -> JSONObject, done: (JSONObject) -> Unit) {
        val token = generation
        executor.execute {
            val result = runCatching(action)
            ui.post {
                if (!visible || token != generation || activity.isFinishing || activity.isDestroyed) return@post
                result.fold(done) { error ->
                    AlertDialog.Builder(activity).setTitle("Bridge settings").setMessage(error.message)
                        .setPositiveButton("Reload") { _, _ -> show() }.setNeutralButton("Pair bridge") { _, _ -> pair() }.setNegativeButton("Close", null).show()
                }
            }
        }
    }

    fun show(message: String = "") {
        visible = true; generation++
        page("Shared bridge settings", message.ifBlank { "Changes apply to every display. Loading…" })
            .addView(button("Back to wall settings") { leave(); back() })
        if (administratorToken().isNullOrBlank()) pair()
        else run({ request("/api/settings") }) { snapshot = it; render(message) }
    }

    private fun pair() {
        val input = EditText(activity).apply {
            setSingleLine(true); hint = "Bridge administrator token"
            inputType = android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_VARIATION_PASSWORD
        }
        AlertDialog.Builder(activity).setTitle("Pair settings access").setMessage("Enter the administrator token shown by the bridge installer. It is saved only for this bridge address.")
            .setView(input).setNegativeButton("Cancel", null).setPositiveButton("Pair") { _, _ ->
                val token = input.text.toString().trim()
                if (token.isNotBlank()) { tokenFile().writeText(token); show() }
            }.show()
    }

    private fun render(message: String) {
        val settings = snapshot.getJSONObject("settings")
        val root = page("Shared bridge settings", message.ifBlank { "These settings affect every display. Wall layout and decoder pins stay on this TV." })
        fun add(label: String, action: () -> Unit) { root.addView(button(label, action)) }
        add("LAN only: ${if (settings.optJSONObject("lan")?.optBoolean("force") == true) "On" else "Off"}") {
            choose("Bridge connections", listOf("LAN only" to true, "Allow other addresses" to false)) { value ->
                patch(JSONObject().put("lan", JSONObject().put("force", value)))
            }
        }
        add("Transcoding: ${settings.optJSONObject("go2rtc")?.optString("transcode", "never") ?: "never"}") {
            choose("Shared transcoding policy", listOf("Pass through" to "never", "When needed" to "auto", "Always" to "always")) {
                patch(JSONObject().put("go2rtc", JSONObject().put("transcode", it)))
            }
        }
        add("Default camera quality: ${settings.optJSONObject("defaults")?.optString("quality", "inherited") ?: "inherited"}") {
            choose("Default quality", listOf("Automatic" to JSONObject.NULL, "Low" to "low", "Medium" to "medium", "High" to "high")) {
                patch(JSONObject().put("defaults", JSONObject().put("quality", it)))
            }
        }
        add("Default camera image") {
            choose("Default image · all dual-lens cameras", imageChoices()) {
                patch(JSONObject().put("defaults", JSONObject().put("dual_view", it)))
            }
        }
        add("Default motion hold duration") {
            number("Default motion hold duration (seconds)", settings.optJSONObject("defaults")?.optInt("hold_seconds", 30) ?: 30) {
                patch(JSONObject().put("defaults", JSONObject().put("hold_seconds", it)))
            }
        }
        val overrides = settings.optJSONObject("cameras") ?: JSONObject()
        val names = cameras().associate { it.sn to it.name }
        val serials = (names.keys + overrides.keys().asSequence().toList()).sortedBy { names[it] ?: it }
        serials.forEach { sn -> add(names[sn] ?: sn) { camera(sn, names[sn] ?: sn, overrides.optJSONObject(sn) ?: JSONObject()) } }
        add("Advanced shared settings") { editJson(settings) }
        add("Export settings backup") {
            run({ request("/api/settings/export") }) { backup ->
                pendingExport = backup.toString(2)
                try { activity.startActivityForResult(Intent(Intent.ACTION_CREATE_DOCUMENT).apply {
                    addCategory(Intent.CATEGORY_OPENABLE); type = "application/json"
                    putExtra(Intent.EXTRA_TITLE, "eufy-wall-settings.json")
                }, EXPORT) } catch (_: Exception) { pendingExport = null; toast("No document provider is available on this TV.") }
            }
        }
        add("Import settings backup") {
            try { activity.startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
                addCategory(Intent.CATEGORY_OPENABLE); type = "application/json"
            }, IMPORT) } catch (_: Exception) { toast("No document provider is available on this TV.") }
        }
        add("Export backup as text") {
            run({ request("/api/settings/export") }) { backup ->
                val json = backup.toString(2)
                val input = EditText(activity).apply { setText(json); maxLines = 12; setTextIsSelectable(true) }
                AlertDialog.Builder(activity).setTitle("Settings backup").setView(input)
                    .setPositiveButton("Copy") { _, _ ->
                        (activity.getSystemService(Activity.CLIPBOARD_SERVICE) as android.content.ClipboardManager)
                            .setPrimaryClip(android.content.ClipData.newPlainText("Eufy Wall settings backup", json))
                        toast("Backup copied")
                    }.setNegativeButton("Close", null).show()
            }
        }
        add("Import backup from text") {
            val input = EditText(activity).apply { hint = "Paste settings backup JSON"; minLines = 5; maxLines = 12 }
            AlertDialog.Builder(activity).setTitle("Import settings backup").setView(input)
                .setNegativeButton("Cancel", null).setPositiveButton("Continue", null).create().apply {
                    setOnShowListener { getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
                        try { val backup = BridgeSettingsProtocol.backup(input.text.toString()); dismiss(); confirmImport(backup) }
                        catch (error: Exception) { input.error = error.message }
                    } }
                }.show()
        }
        add("Pair / change administrator token") { pair() }
        if (snapshot.optJSONObject("apply")?.optString("state") == "restart-required") add("Restart bridge to apply changes") {
            AlertDialog.Builder(activity).setTitle("Restart bridge?").setMessage("All displays will briefly lose their streams.")
                .setNegativeButton("Cancel", null).setPositiveButton("Restart") { _, _ ->
                    run({ request("/api/settings/restart", "POST", JSONObject().put("revision", snapshot.get("revision"))) }) { show("Bridge restart requested. Reload after it returns.") }
                }.show()
        }
        add("Reload") { show() }
        add("Back to wall settings") { leave(); back() }
        root.getChildAt(2)?.requestFocus()
    }

    private fun camera(sn: String, name: String, values: JSONObject) {
        val labels = arrayOf("Power: ${values.optString("power_override", "auto")}",
            "Streaming: ${values.optString("mode", "inherited")}", "Image: ${values.optString("dual_view", "inherited")}",
            "Transcoding: ${values.optString("transcode", "inherited")}", "Enabled: ${values.optBoolean("enabled", true)}",
            "Name", "Quality: ${values.optString("quality", "inherited")}", "Codec: ${values.optString("codec", "inherited")}",
            "Motion hold: ${values.optString("hold_seconds", "inherited")} seconds", "Hardware decode: ${values.optString("vaapi_decode", "inherited")}")
        fun update(key: String, value: Any) = patch(JSONObject().put("cameras", JSONObject().put(sn, JSONObject().put(key, value))))
        AlertDialog.Builder(activity).setTitle("$name · shared").setItems(labels) { _, index ->
            when (index) {
                0 -> choose("Power source", listOf("Automatic" to "auto", "Hardwired / always on" to "always-on", "Battery" to "battery")) { update("power_override", it) }
                1 -> choose("Streaming policy", listOf("Inherit default" to JSONObject.NULL, "Always" to "always", "On motion" to "on_motion", "On demand" to "on_demand")) { update("mode", it) }
                2 -> choose("Requested image · changes every viewer", imageChoices()) { update("dual_view", it) }
                3 -> choose("Camera transcoding", listOf("Inherit default" to JSONObject.NULL, "Pass through" to "never", "When needed" to "auto", "Always" to "always")) { update("transcode", it) }
                4 -> update("enabled", !values.optBoolean("enabled", true))
                5 -> editText("Camera name", values.optString("name", name)) { update("name", it) }
                6 -> choose("Camera quality", listOf("Inherit default" to JSONObject.NULL, "Low" to "low", "Medium" to "medium", "High" to "high")) { update("quality", it) }
                7 -> choose("Camera codec", listOf("Automatic" to JSONObject.NULL, "H.264" to "h264", "H.265" to "h265")) { update("codec", it) }
                8 -> number("Motion hold duration (seconds)", values.optInt("hold_seconds", 30)) { update("hold_seconds", it) }
                9 -> choose("Hardware decode policy", listOf("Inherit default" to JSONObject.NULL, "Hardware first, software fallback" to true, "Software decode" to false)) { update("vaapi_decode", it) }
            }
        }.setNegativeButton("Back", null).show()
    }

    private fun imageChoices(): List<Pair<String, Any>> = listOf("Inherit default" to JSONObject.NULL, "Stacked / Split" to "split", "Single lens" to "single", "PiP top left" to "pip-tl", "PiP top right" to "pip-tr", "PiP bottom left" to "pip-bl", "PiP bottom right" to "pip-br")

    private fun editText(title: String, current: String, saved: (String) -> Unit) {
        val input = EditText(activity).apply { setSingleLine(true); setText(current) }
        AlertDialog.Builder(activity).setTitle(title).setView(input).setNegativeButton("Cancel", null)
            .setPositiveButton("Save") { _, _ -> saved(input.text.toString()) }.show()
    }

    private fun number(title: String, current: Int, saved: (Int) -> Unit) {
        val input = EditText(activity).apply { setSingleLine(true); setText(current.toString()); inputType = android.text.InputType.TYPE_CLASS_NUMBER }
        AlertDialog.Builder(activity).setTitle(title).setView(input).setNegativeButton("Cancel", null).setPositiveButton("Save", null).create().apply {
            setOnShowListener { getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
                val value = input.text.toString().toIntOrNull()
                if (value == null || value <= 0) input.error = "Enter a positive number"
                else { dismiss(); saved(value) }
            } }
        }.show()
    }

    private fun <T> choose(title: String, choices: List<Pair<String, T>>, selected: (T) -> Unit) {
        AlertDialog.Builder(activity).setTitle(title).setItems(choices.map { it.first }.toTypedArray()) { _, i -> selected(choices[i].second) }
            .setNegativeButton("Cancel", null).show()
    }

    private fun patch(changes: JSONObject) {
        val payload = BridgeSettingsProtocol.patch(snapshot, changes)
        run({ request("/api/settings", "PATCH", payload) }) { snapshot = it; render("Saved to bridge. ${applyStatus(it)}") }
    }

    private fun applyStatus(result: JSONObject): String = when (result.optJSONObject("apply")?.optString("state") ?: result.optString("status")) {
        "restart-required" -> "Restart the bridge below to apply these changes."
        "applied" -> "Changes applied."
        "pending" -> "Waiting for the bridge to apply changes."
        "failed" -> "Saved, but applying failed. Reload to inspect the bridge state."
        else -> "Reload to check current state."
    }

    private fun editJson(settings: JSONObject) {
        val input = EditText(activity).apply { setText(settings.toString(2)); setTextColor(-1); minLines = 8; maxLines = 15; setHorizontallyScrolling(false) }
        AlertDialog.Builder(activity).setTitle("Advanced shared settings · JSON merge").setMessage("Changes affect all displays. Use null to clear an override.")
            .setView(input).setNegativeButton("Cancel", null).setPositiveButton("Save", null).create().apply {
                setOnShowListener { getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
                    try { val next = JSONObject(input.text.toString()); dismiss(); patch(next) }
                    catch (_: Exception) { input.error = "Enter a JSON object" }
                } }
            }.show()
    }

    fun result(requestCode: Int, resultCode: Int, data: Intent?): Boolean {
        if (requestCode != EXPORT && requestCode != IMPORT) return false
        if (resultCode != Activity.RESULT_OK || data?.data == null) { pendingExport = null; return true }
        val uri = data.data!!
        if (requestCode == EXPORT) {
            val json = pendingExport ?: return true
            pendingExport = null
            run({ activity.contentResolver.openOutputStream(uri, "wt")?.use { it.write(json.toByteArray(Charsets.UTF_8)) } ?: error("Cannot write backup"); JSONObject() }) { toast("Settings backup saved") }
        } else {
            run({
                val raw = activity.contentResolver.openInputStream(uri)?.use { stream ->
                    val bytes = java.io.ByteArrayOutputStream()
                    val buffer = ByteArray(8192)
                    while (true) {
                        val count = stream.read(buffer)
                        if (count < 0) break
                        require(bytes.size() + count <= 1_048_576) { "Backup is too large" }
                        bytes.write(buffer, 0, count)
                    }
                    String(bytes.toByteArray(), Charsets.UTF_8)
                } ?: error("Cannot read backup")
                BridgeSettingsProtocol.backup(raw)
            }) { backup -> confirmImport(backup) }
        }
        return true
    }

    private fun confirmImport(backup: JSONObject) {
        AlertDialog.Builder(activity).setTitle("Replace shared settings?").setMessage("This backup replaces the bridge settings for every display.")
            .setNegativeButton("Cancel", null).setPositiveButton("Import") { _, _ ->
                val payload = BridgeSettingsProtocol.import(snapshot, backup)
                run({ request("/api/settings/import", "POST", payload) }) { snapshot = it; render("Backup imported. ${applyStatus(it)}") }
            }.show()
    }

    private fun toast(message: String) = Toast.makeText(activity, message, Toast.LENGTH_LONG).show()
    companion object { const val EXPORT = 7201; const val IMPORT = 7202 }
}
