package jp.i32.chain.proximity

import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * ProximityState の配線のうち Android 非依存な不変条件を JVM で確認する。
 * Room/SecureStore/Context に触れる経路 (load の実配線) は instrumented テスト
 * (SightingStoreTest) が担い、ここでは F-01 のガード挙動と、reflection で注入した
 * 状態が now()/tokenAt() に本当に配線されているかを検証する。
 */
class ProximityStateWiringTest {

    @After
    fun reset() {
        setField("windowSec", 900L)
        setField("tokens", emptyList<ProximityState.Token>())
        setField("clockOffsetMs", 0L)
    }

    @Test
    fun `defaults are the documented fallbacks`() {
        assertEquals(900L, ProximityState.windowSec)
        assertEquals(0L, ProximityState.clockOffsetMs)
    }

    @Test
    fun `guarded returns the read value`() {
        assertEquals(5L, ProximityState.guarded("window_sec", 900L) { 5L })
    }

    @Test
    fun `guarded falls back to the default when the secure read throws`() {
        assertEquals(900L, ProximityState.guarded("window_sec", 900L) { throw SecurityException("decrypt failed") })
        assertEquals(0L, ProximityState.guarded("clock_offset_ms", 0L) { throw SecurityException("decrypt failed") })
        assertEquals("[]", ProximityState.guarded("tokens", "[]") { throw SecurityException("decrypt failed") })
    }

    @Test
    fun `now is the wall clock plus the sampled offset`() {
        setField("clockOffsetMs", 12_345L)
        val before = System.currentTimeMillis()
        val now = ProximityState.now()
        assertTrue("now=$now before=$before", now >= before + 12_345L)
    }

    @Test
    fun `tokenAt resolves the loaded token in its own window only`() {
        setField("windowSec", 900L)
        setField("tokens", listOf(ProximityState.Token(0L, byteArrayOf(1, 2, 3))))
        assertNotNull(ProximityState.tokenAt(0L))
        assertNull(ProximityState.tokenAt(900_000L))
    }

    private fun setField(name: String, value: Any?) {
        ProximityState::class.java.getDeclaredField(name).apply {
            isAccessible = true
            set(ProximityState, value)
        }
    }
}
