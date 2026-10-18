package jp.i32.chain.proximity

import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
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
        aggregator.record("boundary", 0L, -50)
        val drained = aggregator.drain(1000L)
        assertFalse(drained.containsKey("boundary"))
    }

    @Test
    fun `entry one millisecond inside the ttl is kept`() {
        val aggregator = SightingAggregator(ttlMs = 1000L)
        aggregator.record("fresh", 1L, -50)
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
        assertEquals(0, aggregator.pending(1000L))
    }

    @Test
    fun `pending counts the live map size`() {
        val aggregator = SightingAggregator()
        assertEquals(0, aggregator.pending(0L))
        aggregator.record("a", 0L, -1)
        aggregator.record("b", 0L, -1)
        assertEquals(2, aggregator.pending(0L))
        aggregator.record("a", 1L, -1)
        assertEquals(2, aggregator.pending(0L))
    }

    @Test
    fun `pending excludes ttl-expired entries before a drain`() {
        val aggregator = SightingAggregator(ttlMs = 1000L)
        aggregator.record("stale", 0L, -50)
        aggregator.record("live", 1L, -50)
        assertEquals(1, aggregator.pending(1000L))
    }

    @Test
    fun `concurrent records merge without lost updates`() {
        val aggregator = SightingAggregator()
        val threads = 8
        val perThread = 250
        val pool = Executors.newFixedThreadPool(threads)
        val start = CountDownLatch(1)
        val done = CountDownLatch(threads)
        try {
            repeat(threads) { t ->
                pool.execute {
                    start.await()
                    repeat(perThread) { i ->
                        aggregator.record("tok", (t * perThread + i).toLong(), -100 + i)
                    }
                    done.countDown()
                }
            }
            start.countDown()
            assertTrue(done.await(10, TimeUnit.SECONDS))
        } finally {
            pool.shutdown()
        }

        val agg = aggregator.drain(1000L).getValue("tok")
        assertEquals(threads * perThread, agg.count)
        assertEquals(0L, agg.firstMs)
        assertEquals((threads * perThread - 1).toLong(), agg.lastMs)
        assertEquals(-100 + perThread - 1, agg.maxRssi)
    }

    @Test
    fun `drain returns exactly the live keys it removed`() {
        val aggregator = SightingAggregator(ttlMs = 1000L)
        aggregator.record("stale", 0L, -50)
        aggregator.record("live", 500L, -50)
        assertEquals(setOf("live"), aggregator.drain(1000L).keys)
    }

    @Test
    fun `a record after drain is returned by the next drain`() {
        val aggregator = SightingAggregator(ttlMs = 1000L)
        aggregator.record("first", 100L, -50)
        assertEquals(setOf("first"), aggregator.drain(100L).keys)

        aggregator.record("second", 150L, -60)
        assertEquals(setOf("second"), aggregator.drain(150L).keys)
        assertEquals(0, aggregator.pending(150L))
    }

    @Test
    fun `a same-token record after drain starts a fresh aggregate`() {
        val aggregator = SightingAggregator(ttlMs = 1000L)
        aggregator.record("tok", 100L, -50)
        assertEquals(1, aggregator.drain(100L).getValue("tok").count)

        aggregator.record("tok", 200L, -60)
        val agg = aggregator.drain(200L).getValue("tok")
        assertEquals(1, agg.count)
        assertEquals(200L, agg.firstMs)
        assertEquals(200L, agg.lastMs)
        assertEquals(-60, agg.maxRssi)
    }
}
