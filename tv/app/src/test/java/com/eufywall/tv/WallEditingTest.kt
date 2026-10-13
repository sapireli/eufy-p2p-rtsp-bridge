package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class WallEditingTest {
    private fun fixed(id: String, camera: String, columnSpan: Int = 1) =
        ConfiguredWallTile(WallTileSpec(id, camera), columnSpan = columnSpan)

    @Test fun changingCameraChecklistPreservesIdentityOrderSpansAndMotionViews() {
        val a = fixed("a", "door", 2)
        val motion = ConfiguredWallTile(WallTileSpec("motion", motion = true, watch = listOf("yard")))
        val b = fixed("b", "garage")
        val old = WallConfiguration(WallLayoutMode.GRID, 3, 2, listOf(a, motion, b))
        val next = WallEditing.withCameras(old, listOf("garage", "door", "yard")) { "new-yard" }
        assertEquals(listOf(a, motion, b), next.tiles.take(3))
        assertEquals(fixed("new-yard", "yard"), next.tiles.last())
        assertEquals(old.layout, next.layout)
        assertEquals(old.columns, next.columns)
        assertEquals(old.rows, next.rows)
    }

    @Test fun selectingExistingCameraKeepsItsDuplicateViewsButDeselectingRemovesThemAll() {
        val config = WallConfiguration(tiles = listOf(fixed("a", "door"), fixed("b", "door"), fixed("c", "garage")))
        assertEquals(config, WallEditing.withCameras(config, listOf("door", "door", "garage")) { error("No additions") })
        assertEquals(listOf(fixed("c", "garage")), WallEditing.withCameras(config, listOf("garage")) { error("No additions") }.tiles)
    }

    @Test fun blankAndDuplicateSelectionsDoNotCreateExtraViews() {
        var counter = 0
        val next = WallEditing.withCameras(WallConfiguration(), listOf("", "door", "door", "garage")) { "id-${counter++}" }
        assertEquals(listOf("door", "garage"), next.tiles.map { it.spec.camera })
        assertEquals(2, counter)
    }

    @Test fun reusedOrBlankNewIdsAreRejected() {
        val old = WallConfiguration(tiles = listOf(fixed("same", "door")))
        try { WallEditing.withCameras(old, listOf("door", "garage")) { "same" }; fail("Duplicate ID accepted") }
        catch (_: IllegalArgumentException) { }
        try { WallEditing.withCameras(WallConfiguration(), listOf("door")) { "" }; fail("Blank ID accepted") }
        catch (_: IllegalArgumentException) { }
    }

    @Test fun addingThirdCameraExpandsTwoColumnGridWithoutChangingExistingTiles() {
        val a = fixed("a", "door")
        val b = fixed("b", "garage")
        val old = WallConfiguration(WallLayoutMode.GRID, 2, 1, listOf(a, b))
        val next = WallEditing.withCameras(old, listOf("door", "garage", "yard")) { "yard" }
        assertEquals(2, next.columns)
        assertEquals(2, next.rows)
        assertEquals(listOf(a, b), next.tiles.take(2))
        assertTrue(next.validationErrors().isEmpty())
    }

    @Test fun addingCameraPreservesExplicitCustomSpanWhileGrowingRows() {
        val wide = fixed("wide", "door", 2).copy(column = 0, row = 0)
        val motion = ConfiguredWallTile(WallTileSpec("motion", motion = true))
        val old = WallConfiguration(WallLayoutMode.GRID, 2, 2, listOf(wide, motion, fixed("garage", "garage")))
        val next = WallEditing.withCameras(old, listOf("door", "garage", "yard")) { "yard" }
        assertEquals(3, next.rows)
        assertEquals(2, next.columns)
        assertEquals(wide, next.tiles.first())
        assertEquals(motion, next.tiles[1])
        assertTrue(next.validationErrors().isEmpty())
    }

    @Test fun equalGridPresetResetsGeometryAndKeepsSourcesAndOrder() {
        val old = WallConfiguration(tiles = listOf(fixed("a", "door", 2).copy(column = 1, row = 2, rowSpan = 3),
            ConfiguredWallTile(WallTileSpec("motion", motion = true)), fixed("b", "garage")))
        val next = WallEditing.presetColumns(old, 2)
        assertEquals(WallLayoutMode.GRID, next.layout)
        assertEquals(2, next.columns)
        assertEquals(2, next.rows)
        assertEquals(old.tiles.map { it.spec }, next.tiles.map { it.spec })
        next.tiles.forEach { assertEquals(-1, it.column); assertEquals(-1, it.row); assertEquals(1, it.columnSpan); assertEquals(1, it.rowSpan) }
        assertTrue(next.validationErrors().isEmpty())
    }

    @Test fun gridPresetEnforcesSixRowLimitAndSupportsThirtySixViews() {
        val config = WallConfiguration(tiles = (0 until 36).map { fixed("$it", "camera-$it") })
        try { WallEditing.presetColumns(config, 4); fail("Nine-row preset accepted") }
        catch (e: IllegalArgumentException) { assertTrue(e.message!!.contains("choose more columns")) }
        val full = WallEditing.presetColumns(config, 6)
        assertEquals(6, full.rows)
        assertTrue(full.validationErrors().isEmpty())
        assertEquals(1, WallEditing.presetColumns(WallConfiguration(), 2).rows)
    }
}
