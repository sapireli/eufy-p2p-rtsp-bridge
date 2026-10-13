package com.eufywall.tv

import android.content.Context
import android.view.ViewGroup
import kotlin.math.roundToInt

/** Measures each tile directly: spans never depend on GridLayout's child-content weights. */
class WallCanvas(context: Context, private val tiles: List<WallTileBounds>) : ViewGroup(context) {
    init { setBackgroundColor(0xff000000.toInt()) }
    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        val w = MeasureSpec.getSize(widthMeasureSpec)
        val h = MeasureSpec.getSize(heightMeasureSpec)
        setMeasuredDimension(w, h)
        for (i in 0 until childCount) {
            val tile = tiles[i]
            val left = (tile.x * w).roundToInt() + 2
            val right = ((tile.x + tile.width) * w).roundToInt() - 2
            val top = (tile.y * h).roundToInt() + 2
            val bottom = ((tile.y + tile.height) * h).roundToInt() - 2
            getChildAt(i).measure(MeasureSpec.makeMeasureSpec((right - left).coerceAtLeast(0), MeasureSpec.EXACTLY),
                MeasureSpec.makeMeasureSpec((bottom - top).coerceAtLeast(0), MeasureSpec.EXACTLY))
        }
    }
    override fun onLayout(changed: Boolean, left: Int, top: Int, right: Int, bottom: Int) {
        val w = right - left; val h = bottom - top
        for (i in 0 until childCount) {
            val tile = tiles[i]
            getChildAt(i).layout((tile.x * w).roundToInt() + 2, (tile.y * h).roundToInt() + 2,
                ((tile.x + tile.width) * w).roundToInt() - 2, ((tile.y + tile.height) * h).roundToInt() - 2)
        }
    }
}
