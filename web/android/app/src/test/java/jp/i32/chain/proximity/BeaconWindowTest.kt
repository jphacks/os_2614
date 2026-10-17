package jp.i32.chain.proximity

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class BeaconWindowTest {

    private val windowSec = 900L

    // 900 * 2_000_000 = 1_800_000_000 (ちょうど窓境界)
    private val windowStart = 1_800_000_000L

    @Test
    fun `nowMs exactly at a window start selects that window`() {
        val nowMs = windowStart * 1000
        assertEquals(windowStart, BeaconWindow.windowStartSec(nowMs, windowSec))
    }

    @Test
    fun `nowMs 1ms before the next boundary stays in the same window`() {
        val nextStartMs = (windowStart + windowSec) * 1000
        val nowMs = nextStartMs - 1
        assertEquals(windowStart, BeaconWindow.windowStartSec(nowMs, windowSec))
    }

    @Test
    fun `nowMs at windowStart + windowSec rolls to the next window`() {
        val nowMs = (windowStart + windowSec) * 1000
        assertEquals(windowStart + windowSec, BeaconWindow.windowStartSec(nowMs, windowSec))
    }

    @Test
    fun `mirrors floorDiv for negative nowMs`() {
        // -1000 / 1000 = -1, floorDiv(-1, 900) = -1, *900 = -900
        assertEquals(-900L, BeaconWindow.windowStartSec(-1000L, windowSec))
    }

    @Test
    fun `tokenFor returns the token whose window matches`() {
        val bytes = byteArrayOf(1, 2, 3)
        val tokens = listOf(windowStart to bytes)
        val found = BeaconWindow.tokenFor(tokens, windowStart * 1000, windowSec)
        assertEquals(windowStart, found!!.first)
        assertArrayEquals(bytes, found.second)
    }

    @Test
    fun `tokenFor returns null when no token matches the window`() {
        val tokens = listOf((windowStart + windowSec) to byteArrayOf(9))
        assertNull(BeaconWindow.tokenFor(tokens, windowStart * 1000, windowSec))
    }

    @Test
    fun `tokenFor returns null for an empty token list`() {
        assertNull(BeaconWindow.tokenFor(emptyList(), windowStart * 1000, windowSec))
    }
}
