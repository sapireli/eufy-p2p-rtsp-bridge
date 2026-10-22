package com.eufywall.tv

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class BridgeSettingsProtocolTest {
    @Test fun overrideRemovalKeepsRevisionAndNullWithoutSendingOtherSettings() {
        val snapshot = JSONObject("""{"revision":7,"settings":{"lan":{"force":true}}}""")
        val changes = JSONObject("""{"cameras":{"camera-1":{"dual_view":null}}}""")
        val payload = BridgeSettingsProtocol.patch(snapshot, changes)
        assertEquals(7, payload.getInt("revision"))
        assertTrue(payload.getJSONObject("settings").getJSONObject("cameras").getJSONObject("camera-1").isNull("dual_view"))
        assertFalse(payload.getJSONObject("settings").has("lan"))
    }

    @Test fun importUsesCurrentRevisionRatherThanBackupRevision() {
        val backup = BridgeSettingsProtocol.backup("""{"format":"eufy-wall-settings","version":1,"settings":{"cameras":{}}}""")
        val payload = BridgeSettingsProtocol.import(JSONObject("""{"revision":9}"""), backup)
        assertEquals(9, payload.getInt("revision"))
        assertEquals(backup, payload.getJSONObject("backup"))
        assertFalse(payload.has("settings"))
    }

    @Test fun rejectsUnsupportedOrIncompleteBackups() {
        listOf("{}", """{"format":"eufy-wall-settings","version":2,"settings":{}}""", """{"format":"eufy-wall-settings","version":1}""", """{"format":"eufy-wall-settings","version":1,"settings":{},"extra":true}""").forEach {
            assertThrows(IllegalArgumentException::class.java) { BridgeSettingsProtocol.backup(it) }
        }
    }
}
