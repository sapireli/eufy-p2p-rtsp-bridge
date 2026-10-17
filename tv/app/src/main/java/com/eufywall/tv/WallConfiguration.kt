package com.eufywall.tv

import org.json.JSONArray
import org.json.JSONObject

enum class WallLayoutMode { AUTO, GRID }

/** A negative column and row requests automatic placement in the configured grid. */
data class ConfiguredWallTile(
    val spec: WallTileSpec,
    val column: Int = -1,
    val row: Int = -1,
    val columnSpan: Int = 1,
    val rowSpan: Int = 1
)

/** Versioned configuration independent of the available cameras and their current state. */
data class WallConfiguration(
    val layout: WallLayoutMode = WallLayoutMode.AUTO,
    val columns: Int = 2,
    val rows: Int = 2,
    val tiles: List<ConfiguredWallTile> = emptyList(),
    val motionFlash: Boolean = true
) {
    fun validationErrors(): List<String> {
        val errors = mutableListOf<String>()
        if (columns !in 1..6 || rows !in 1..6) errors += "Grid dimensions must be between 1 and 6"
        if (tiles.size > 36) errors += "A wall supports at most 36 tiles"
        if (tiles.map { it.spec.id }.distinct().size != tiles.size) errors += "Tile IDs must be unique"
        tiles.forEach { tile ->
            if (tile.spec.id.isBlank()) errors += "Tile ID is required"
            if (!tile.spec.motion && tile.spec.camera.isBlank()) errors += "Fixed tiles require a camera"
            if (tile.spec.dwellMs < 0 || tile.spec.blankAfterMs < 0) errors += "Motion timing cannot be negative"
            if (tile.columnSpan !in 1..6 || tile.rowSpan !in 1..6) errors += "Tile spans must be between 1 and 6"
            if ((tile.column == -1) != (tile.row == -1) || tile.column !in -1..5 || tile.row !in -1..5)
                errors += "Specify both tile coordinates or use automatic placement"
        }
        if (errors.isEmpty() && layout == WallLayoutMode.GRID) {
            try { WallLayout.grid(this) } catch (e: IllegalArgumentException) { errors += e.message ?: "Invalid grid" }
        }
        return errors
    }

    fun encode(): String {
        require(validationErrors().isEmpty()) { validationErrors().joinToString("; ") }
        return JSONObject().put("version", 1).put("layout", layout.name)
            .put("motionFlash", motionFlash).put("columns", columns).put("rows", rows).put("tiles", JSONArray().apply {
                tiles.forEach { tile ->
                    put(JSONObject().put("id", tile.spec.id).put("camera", tile.spec.camera)
                        .put("motion", tile.spec.motion).put("watch", JSONArray(tile.spec.watch))
                        .put("dwellMs", tile.spec.dwellMs).put("blankAfterMs", tile.spec.blankAfterMs)
                        .put("column", tile.column).put("row", tile.row)
                        .put("columnSpan", tile.columnSpan).put("rowSpan", tile.rowSpan))
                }
            }).toString()
    }

    companion object {
        /** Corrupt or future configurations are rejected; the caller can preserve legacy selection. */
        fun decode(raw: String?): WallConfiguration? = try {
            if (raw.isNullOrBlank()) null else {
                val root = JSONObject(raw)
                require(root.getInt("version") == 1)
                val array = root.getJSONArray("tiles")
                require(array.length() <= 36)
                val config = WallConfiguration(
                    WallLayoutMode.valueOf(root.getString("layout")), root.getInt("columns"), root.getInt("rows"),
                    (0 until array.length()).map { index ->
                        val tile = array.getJSONObject(index)
                        val watch = tile.optJSONArray("watch") ?: JSONArray()
                        ConfiguredWallTile(WallTileSpec(tile.getString("id"), tile.optString("camera", ""),
                            tile.optBoolean("motion", false), (0 until watch.length()).map { watch.getString(it) },
                            tile.optLong("dwellMs", 5000), tile.optLong("blankAfterMs", 30000)),
                            tile.optInt("column", -1), tile.optInt("row", -1),
                            tile.optInt("columnSpan", 1), tile.optInt("rowSpan", 1))
                    }, root.optBoolean("motionFlash", true))
                require(config.validationErrors().isEmpty())
                config
            }
        } catch (_: Exception) { null }

        fun fromLegacyChosen(cameras: List<String>): WallConfiguration = WallConfiguration(
            tiles = cameras.filter { it.isNotBlank() }.distinct().take(36).mapIndexed { index, camera ->
                ConfiguredWallTile(WallTileSpec(id = "tile-$index", camera = camera))
            })
    }
}
