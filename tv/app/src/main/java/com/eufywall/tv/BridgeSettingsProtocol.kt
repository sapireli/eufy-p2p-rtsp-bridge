package com.eufywall.tv

import org.json.JSONObject

internal object BridgeSettingsProtocol {
    fun patch(snapshot: JSONObject, changes: JSONObject): JSONObject =
        JSONObject().put("revision", snapshot.get("revision")).put("settings", changes)

    fun backup(json: String): JSONObject {
        require(json.length <= 1_048_576) { "Backup is too large" }
        val backup = JSONObject(json)
        require(backup.optString("format") == "eufy-wall-settings" && backup.optInt("version") == 1 && backup.optJSONObject("settings") != null && backup.keys().asSequence().all { it in setOf("format", "version", "settings") }) { "Unsupported settings backup" }
        return backup
    }

    fun import(snapshot: JSONObject, backup: JSONObject): JSONObject =
        JSONObject().put("revision", snapshot.get("revision")).put("backup", backup)
}
