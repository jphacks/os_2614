package jp.i32.chain.proximity

import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith

/**
 * statusJson() は Capacitor の JSObject (org.json.JSONObject) を使うため JVM 単体テストでは
 * "Method put in org.json.JSONObject not mocked" で動かない。キー集合の検証だけを実機/エミュレータで行う。
 */
@RunWith(AndroidJUnit4::class)
class ProximityStateJsonInstrumentedTest {

    @Test
    fun statusJsonExposesExactlyTheNineSpecKeys() {
        val expected = setOf(
            "running",
            "bluetoothEnabled",
            "advertising",
            "scanning",
            "currentWindowStart",
            "tokensValidUntil",
            "pendingSightings",
            "clockOffsetMs",
            "lastError",
        )
        val keys = ProximityState.statusJson().keys().asSequence().toSet()
        assertEquals(expected, keys)
    }
}
