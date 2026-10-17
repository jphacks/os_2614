package jp.i32.chain.proximity

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SightingAggregatorTest {

    @Test
    fun `three records merge into one aggregate`() {
        val aggregator = SightingAggregator()
        aggregator.record("tok", 100L, -70)
        aggregator.record("tok", 200L, -60)
        aggregator.record("tok", 150L, -80)

        val drained = aggregator.drain(200L)
        val agg = drained.getValue("tok")
        assertEquals(100L, agg.firstMs)
        assertEquals(200L, agg.lastMs)
        assertEquals(-60, agg.maxRssi)
        assertEquals(3, agg.count)
        assertEquals(1, drained.size)
    }

    @Test
    fun `entry exactly at the ttl boundary is dropped`() {
        val aggregator = SightingAggregator(ttlMs = 1000L)
        aggregator.record("boundary", 0L, -50) // lastMs == nowMs - ttlMs
        val drained = aggregator.drain(1000L)
        assertFalse(drained.containsKey("boundary"))
    }

    @Test
    fun `entry one millisecond inside the ttl is kept`() {
        val aggregator = SightingAggregator(ttlMs = 1000L)
        aggregator.record("fresh", 1L, -50) // lastMs == nowMs - ttlMs + 1
        val drained = aggregator.drain(1000L)
        assertTrue(drained.containsKey("fresh"))
        assertEquals(1, drained.size)
    }

    @Test
    fun `drain removes expired and returned entries`() {
        val aggregator = SightingAggregator(ttlMs = 1000L)
        aggregator.record("stale", 0L, -50)
        aggregator.record("live", 500L, -50)

        val drained = aggregator.drain(1000L)
        assertEquals(1, drained.size)
        assertFalse(drained.containsKey("stale"))
        assertTrue(drained.containsKey("live"))
        assertEquals(0, aggregator.pending())
    }

    @Test
    fun `pending reflects the map size`() {
        val aggregator = SightingAggregator()
        assertEquals(0, aggregator.pending())
        aggregator.record("a", 0L, -1)
        aggregator.record("b", 0L, -1)
        assertEquals(2, aggregator.pending())
        aggregator.record("a", 1L, -1)
        assertEquals(2, aggregator.pending())
    }
}
