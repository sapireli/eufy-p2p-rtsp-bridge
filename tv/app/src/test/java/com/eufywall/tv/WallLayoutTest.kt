package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class WallLayoutTest {
    @Test fun hardwarePinsPersistAndSurviveEditsWithLegacyDefaultEmpty() {
        val original = WallConfiguration(hardwarePinnedCameraIds = setOf("door", "balcony"))
        assertEquals(original, WallConfiguration.decode(original.encode()))
        assertEquals(setOf("door", "balcony"), original.copy(motionFlash = false).hardwarePinnedCameraIds)
        val old = """{"version":1,"layout":"AUTO","columns":2,"rows":2,"tiles":[]}"""
        assertTrue(WallConfiguration.decode(old)!!.hardwarePinnedCameraIds.isEmpty())
    }
    private fun tile(id: String, column: Int = -1, row: Int = -1, cs: Int = 1, rs: Int = 1) =
        ConfiguredWallTile(WallTileSpec(id = id, camera = "camera-$id"), column, row, cs, rs)

    @Test fun portraitPairPreservesSeventyThirtyAndEmptyFourthArea() {
        val config = WallConfiguration(tiles = listOf(tile("door"), tile("garage"), tile("balcony")))
        val bounds = WallLayout.calculate(config, setOf("camera-door", "camera-garage"))
        assertEquals(.35, bounds[0].width, .000001)
        assertEquals(.35, bounds[1].x, .000001)
        assertEquals(.7, bounds[2].x, .000001)
        assertEquals(.5, bounds[2].height, .000001)
        assertEquals(1.0, bounds[0].height, .000001)
    }

    @Test fun twoTallRightStacksOneToThreeLandscapeFeedsAndPersists() {
        for (count in 2..5) {
            val config = WallConfiguration(layout = WallLayoutMode.TWO_TALL_RIGHT, tiles = (0 until count).map { tile("$it") })
            assertEquals(config, WallConfiguration.decode(config.encode()))
            val bounds = WallLayout.calculate(config)
            assertEquals(.35, bounds[0].width, .000001)
            assertEquals(.35, bounds[1].x, .000001)
            assertEquals(1.0, bounds[1].height, .000001)
            bounds.drop(2).forEachIndexed { i, b ->
                assertEquals(.7, b.x, .000001)
                assertEquals(.3, b.width, .000001)
                assertEquals(1.0 / (count - 2), b.height, .000001)
                assertEquals(i.toDouble() / (count - 2), b.y, .000001)
            }
        }
        for (count in listOf(0, 1, 6)) assertFalse(WallConfiguration(layout = WallLayoutMode.TWO_TALL_RIGHT,
            tiles = (0 until count).map { tile("$it") }).validationErrors().isEmpty())
    }

    @Test fun thirtySixTilesFitWithoutOverlap() {
        val bounds = WallLayout.calculate(WallConfiguration(tiles = (0 until 36).map { tile("$it") }))
        assertEquals(36, bounds.size)
        bounds.forEach { b ->
            assertTrue(b.x >= 0 && b.y >= 0 && b.x + b.width <= 1.000001 && b.y + b.height <= 1.000001)
        }
        bounds.forEachIndexed { i, a -> bounds.drop(i + 1).forEach { b ->
            assertFalse(a.x < b.x + b.width - .000001 && b.x < a.x + a.width - .000001 &&
                a.y < b.y + b.height - .000001 && b.y < a.y + a.height - .000001)
        } }
    }

    @Test fun explicitSpansReservedBeforeAutomaticPlacement() {
        val config = WallConfiguration(WallLayoutMode.GRID, 3, 2,
            listOf(tile("auto"), tile("span", 0, 0, 2, 2), tile("last")))
        assertTrue(config.validationErrors().isEmpty())
        val bounds = WallLayout.calculate(config)
        assertEquals(2.0 / 3, bounds[0].x, .000001)
        assertEquals(0.0, bounds[0].y, .000001)
        assertEquals(.5, bounds[2].y, .000001)
    }

    @Test fun overlapBoundsAndCapacityRejected() {
        listOf(
            WallConfiguration(WallLayoutMode.GRID, 2, 2, listOf(tile("a", 0, 0, 2, 2), tile("b", 1, 1))),
            WallConfiguration(WallLayoutMode.GRID, 2, 2, listOf(tile("a", 1, 0, 2, 1))),
            WallConfiguration(WallLayoutMode.GRID, 1, 1, listOf(tile("a"), tile("b"))),
            WallConfiguration(tiles = (0..36).map { tile("$it") }),
            WallConfiguration(tiles = listOf(tile("a", 0, -1))),
            WallConfiguration(WallLayoutMode.GRID, 2, 2, listOf(tile("a", Int.MAX_VALUE, 0))),
            WallConfiguration(columns = 0)
        ).forEach { assertFalse(it.validationErrors().isEmpty()) }
    }

    @Test fun jsonRoundtripIncludesMotionWatchAndGeometry() {
        val motion = WallTileSpec("motion tile", motion = true, watch = listOf("SN\"quoted", "SN2"),
            dwellMs = 1234, blankAfterMs = 6789)
        val config = WallConfiguration(WallLayoutMode.GRID, 3, 2,
            listOf(ConfiguredWallTile(motion, 0, 0, 2, 2), tile("fixed", 2, 0)))
        assertEquals(config, WallConfiguration.decode(config.encode()))
    }

    @Test fun identityAndTimingValidationRejectsUnsafePersistence() {
        assertFalse(WallConfiguration(tiles = listOf(tile("a"), tile("a"))).validationErrors().isEmpty())
        assertFalse(WallConfiguration(tiles = listOf(ConfiguredWallTile(WallTileSpec("blank")))).validationErrors().isEmpty())
        assertFalse(WallConfiguration(tiles = listOf(ConfiguredWallTile(
            WallTileSpec("motion", motion = true, dwellMs = -1)))).validationErrors().isEmpty())
        assertTrue(WallConfiguration(tiles = listOf(ConfiguredWallTile(
            WallTileSpec("motion", motion = true)))).validationErrors().isEmpty())
    }

    @Test fun gridSpansPersistAndReorderingChangesOnlyAutomaticPositions() {
        val a = tile("a", cs = 2)
        val b = tile("b")
        val config = WallConfiguration(WallLayoutMode.GRID, 3, 1, listOf(a, b))
        assertEquals(config, WallConfiguration.decode(config.encode()))
        assertEquals(0.0, WallLayout.calculate(config)[0].x, .000001)
        val moved = config.copy(tiles = listOf(b, a))
        assertEquals(1.0 / 3, WallLayout.calculate(moved)[1].x, .000001)
        assertEquals(2.0 / 3, WallLayout.calculate(moved)[1].width, .000001)
    }

    @Test fun malformedFutureAndInvalidLayoutsDoNotDestroyLegacySelection() {
        assertNull(WallConfiguration.decode("not-json"))
        assertNull(WallConfiguration.decode("{\"version\":99}"))
        assertNull(WallConfiguration.decode("{\"version\":1,\"layout\":\"GRID\",\"columns\":9,\"rows\":1,\"tiles\":[]}"))
        val legacy = WallConfiguration.fromLegacyChosen(listOf("door", "", "garage", "door", "balcony", "yard", "fifth"))
        assertEquals(listOf("door", "garage", "balcony", "yard", "fifth"), legacy.tiles.map { it.spec.camera })
        assertEquals(WallLayoutMode.AUTO, legacy.layout)
    }
}
