package jp.i32.chain.proximity

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * ProximityState の配線のうち Android 非依存な不変条件だけを JVM で確認する。
 * Room/SecureStore/Context に触れる経路は instrumented テストと各 unit テストが担う。
 */
class ProximityStateWiringTest {

    @Test
    fun `defaults are the documented fallbacks before load`() {
        assertEquals(900L, ProximityState.windowSec)
        assertEquals(0L, ProximityState.clockOffsetMs)
    }

    @Test
    fun `now stays the wall clock plus the sampled offset`() {
        val before = System.currentTimeMillis()
        val now = ProximityState.now()
        val after = System.currentTimeMillis()
        assertTrue("now=$now before=$before after=$after", now in before..after)
    }

    @Test
    fun `tokenAt is null while no tokens are loaded`() {
        assertNull(ProximityState.tokenAt(System.currentTimeMillis()))
    }
}
