package com.eufywall.tv

import kotlin.math.ceil
import kotlin.math.sqrt

/** Normalized geometry avoids depending on the device resolution or Android view system. */
data class WallTileBounds(val tile: ConfiguredWallTile, val x: Double, val y: Double,
                          val width: Double, val height: Double)

object WallLayout {
    fun calculate(config: WallConfiguration, portraitCameraIds: Set<String> = emptySet()): List<WallTileBounds> {
        require(config.validationErrors().isEmpty()) { config.validationErrors().joinToString("; ") }
        return when (config.layout) {
            WallLayoutMode.GRID -> grid(config)
            WallLayoutMode.TWO_TALL_RIGHT -> twoTallRight(config.tiles)
            WallLayoutMode.AUTO -> automatic(config.tiles, portraitCameraIds)
        }
    }

    /** First two selected cameras are full height; remaining feeds stack on the right. */
    private fun twoTallRight(tiles: List<ConfiguredWallTile>): List<WallTileBounds> {
        val wideCount = tiles.size - 2
        return tiles.take(2).mapIndexed { i, tile -> WallTileBounds(tile, i * .35, 0.0, .35, 1.0) } +
            tiles.drop(2).mapIndexed { i, tile -> WallTileBounds(tile, .7, i.toDouble() / wideCount, .3, 1.0 / wideCount) }
    }

    private fun automatic(tiles: List<ConfiguredWallTile>, portraits: Set<String>): List<WallTileBounds> {
        if (tiles.isEmpty()) return emptyList()
        val tall = tiles.filter { !it.spec.motion && it.spec.camera in portraits }
        val wide = tiles.filterNot { it in tall }
        if (tiles.size in 3..4 && tall.size == 2 && wide.isNotEmpty()) {
            return tall.mapIndexed { i, tile -> WallTileBounds(tile, i * .35, 0.0, .35, 1.0) } +
                wide.mapIndexed { i, tile -> WallTileBounds(tile, .7, i * .5, .3, .5) }
        }
        if (tiles.size == 3 && tall.size == 1) {
            return listOf(WallTileBounds(tall[0], 0.0, 0.0, .4, 1.0)) +
                wide.mapIndexed { i, tile -> WallTileBounds(tile, .4, i * .5, .6, .5) }
        }
        val columns = if (tiles.size <= 2) tiles.size else ceil(sqrt(tiles.size.toDouble())).toInt()
        val rows = ceil(tiles.size.toDouble() / columns).toInt()
        return tiles.mapIndexed { i, tile -> WallTileBounds(tile,
            (i % columns).toDouble() / columns, (i / columns).toDouble() / rows,
            1.0 / columns, 1.0 / rows) }
    }

    /** Reserve explicit spans first, then row-major pack automatic tiles without overlap. */
    internal fun grid(config: WallConfiguration): List<WallTileBounds> {
        require(config.columns in 1..6 && config.rows in 1..6) { "Invalid grid dimensions" }
        val occupied = Array(config.rows) { BooleanArray(config.columns) }
        val positions = mutableMapOf<String, Pair<Int, Int>>()
        fun fits(tile: ConfiguredWallTile, column: Int, row: Int): Boolean =
            column in 0 until config.columns && row in 0 until config.rows && tile.columnSpan > 0 && tile.rowSpan > 0 &&
                column + tile.columnSpan <= config.columns && row + tile.rowSpan <= config.rows &&
                (row until row + tile.rowSpan).all { y ->
                    (column until column + tile.columnSpan).all { x -> !occupied[y][x] }
                }
        fun place(tile: ConfiguredWallTile, column: Int, row: Int) {
            require(fits(tile, column, row)) { "Tile ${tile.spec.id} exceeds the grid or overlaps another tile" }
            for (y in row until row + tile.rowSpan) for (x in column until column + tile.columnSpan) occupied[y][x] = true
            positions[tile.spec.id] = column to row
        }
        config.tiles.filter { it.column >= 0 }.forEach { place(it, it.column, it.row) }
        config.tiles.filter { it.column < 0 }.forEach { tile ->
            val position = (0 until config.rows).flatMap { y -> (0 until config.columns).map { x -> x to y } }
                .firstOrNull { (x, y) -> fits(tile, x, y) }
                ?: throw IllegalArgumentException("No grid space for tile ${tile.spec.id}")
            place(tile, position.first, position.second)
        }
        return config.tiles.map { tile ->
            val (x, y) = positions.getValue(tile.spec.id)
            WallTileBounds(tile, x.toDouble() / config.columns, y.toDouble() / config.rows,
                tile.columnSpan.toDouble() / config.columns, tile.rowSpan.toDouble() / config.rows)
        }
    }
}
