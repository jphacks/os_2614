package jp.i32.chain.proximity

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ClockSamplerTest {

    @Test
    fun `single sample returns that sample`() {
        val sampler = ClockSampler()
        assertEquals(50L, sampler.add(50L))
        assertEquals(50L, sampler.current())
    }

    @Test
    fun `five samples return the median`() {
        val sampler = ClockSampler()
        listOf(10L, 20L, 30L, 40L, 50L).forEach { sampler.add(it) }
        assertEquals(30L, sampler.current())
    }

    @Test
    fun `outlier is tolerated by the median`() {
        val sampler = ClockSampler()
        var last = 0L
        listOf(0L, 100L, 100L, 100L, 100000L).forEach { last = sampler.add(it) }
        assertEquals(100L, last)
        assertEquals(100L, sampler.current())
    }

    @Test
    fun `ring eviction keeps only the last samples`() {
        val sampler = ClockSampler(samples = 3)
        listOf(1L, 2L, 3L, 4L, 5L).forEach { sampler.add(it) }
        // keeps [3,4,5] -> median 4
        assertEquals(4L, sampler.current())
    }

    @Test
    fun `shouldWarn is false at exactly the threshold and true above it`() {
        fun warnAt(value: Long): Boolean {
            val sampler = ClockSampler()
            sampler.add(value)
            return sampler.shouldWarn()
        }

        assertFalse(warnAt(60_000L))
        assertTrue(warnAt(60_001L))
        assertFalse(warnAt(-60_000L))
        assertTrue(warnAt(-60_001L))
    }

    @Test
    fun `empty sampler returns zero and does not warn`() {
        val sampler = ClockSampler()
        assertEquals(0L, sampler.current())
        assertFalse(sampler.shouldWarn())
    }

    @Test
    fun `clear resets samples and current offset`() {
        val sampler = ClockSampler()
        sampler.add(120_000L)
        sampler.clear()
        assertEquals(0L, sampler.current())
        assertFalse(sampler.shouldWarn())
    }
}
