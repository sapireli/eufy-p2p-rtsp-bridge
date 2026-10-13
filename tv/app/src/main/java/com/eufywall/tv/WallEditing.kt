package com.eufywall.tv

/** Editing operations preserve tile identity so camera selection does not erase custom layouts. */
object WallEditing {
    fun withCameras(config: WallConfiguration, selected: List<String>, newId: () -> String): WallConfiguration {
        val wanted = selected.filter { it.isNotBlank() }.distinct()
        val retained = config.tiles.filter { it.spec.motion || it.spec.camera in wanted }
        val existing = retained.filterNot { it.spec.motion }.map { it.spec.camera }.toSet()
        val ids = retained.map { it.spec.id }.toMutableSet()
        val added = wanted.filterNot { it in existing }.map { camera ->
            val id = newId()
            require(id.isNotBlank() && ids.add(id)) { "Each view needs a unique ID" }
            ConfiguredWallTile(WallTileSpec(id = id, camera = camera))
        }
        require(retained.size + added.size <= 36) { "A wall supports up to 36 views" }
        val candidate = config.copy(tiles = retained + added)
        if (candidate.layout != WallLayoutMode.GRID || candidate.validationErrors().isEmpty()) return candidate
        // Grow only the vertical canvas; all existing positions, identities and spans stay intact.
        for (rows in (candidate.rows + 1)..6) {
            val expanded = candidate.copy(rows = rows)
            if (expanded.validationErrors().isEmpty()) return expanded
        }
        throw IllegalArgumentException("This grid has no room for the selected cameras; choose more columns or smaller views")
    }

    /** Choosing an equal-sized preset intentionally replaces earlier custom cell geometry. */
    fun presetColumns(config: WallConfiguration, columns: Int): WallConfiguration {
        require(columns in 1..6) { "Choose between 1 and 6 columns" }
        val rows = ((config.tiles.size + columns - 1) / columns).coerceAtLeast(1)
        require(rows <= 6) { "${config.tiles.size} views need more than 6 rows; choose more columns" }
        return config.copy(layout = WallLayoutMode.GRID, columns = columns, rows = rows,
            tiles = config.tiles.map { it.copy(column = -1, row = -1, columnSpan = 1, rowSpan = 1) })
    }
}
