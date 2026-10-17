package jp.i32.chain.proximity

import org.junit.Assert.assertEquals
import org.junit.Test

class ClockOffsetEstimatorTest {

    @Test
    fun `single sample returns that sample`() {
        val estimator = ClockOffsetEstimator()
        assertEquals(50L, estimator.add(50L))
        assertEquals(50L, estimator.current())
    }

    @Test
    fun `five samples return the median`() {
        val estimator = ClockOffsetEstimator()
        listOf(10L, 20L, 30L, 40L, 50L).forEach { estimator.add(it) }
        assertEquals(30L, estimator.current())
    }

    @Test
    fun `even count returns the lower median`() {
        val estimator = ClockOffsetEstimator()
        listOf(10L, 20L, 30L, 40L).forEach { estimator.add(it) }
        // sorted = [10,20,30,40], size/2 = 2 -> 30
        assertEquals(30L, estimator.current())
    }

    @Test
    fun `outlier is ignored by the median`() {
        val estimator = ClockOffsetEstimator()
        val values = listOf(0L, 100L, 100L, 100L, 100000L)
        var last = 0L
        values.forEach { last = estimator.add(it) }
        assertEquals(100L, last)
        assertEquals(100L, estimator.current())
    }

    @Test
    fun `ring eviction keeps only the last maxSamples samples`() {
        val estimator = ClockOffsetEstimator(maxSamples = 3)
        listOf(1L, 2L, 3L, 4L, 5L).forEach { estimator.add(it) }
        // keeps [3,4,5] -> median 4
        assertEquals(4L, estimator.current())
    }

    @Test
    fun `empty estimator returns zero`() {
        assertEquals(0L, ClockOffsetEstimator().current())
    }

    @Test
    fun `clear resets the estimator`() {
        val estimator = ClockOffsetEstimator()
        estimator.add(42L)
        estimator.clear()
        assertEquals(0L, estimator.current())
    }
}
