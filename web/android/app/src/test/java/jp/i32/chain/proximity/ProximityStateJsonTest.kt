package jp.i32.chain.proximity

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ProximityStateJsonTest {

    private val specVocabulary = setOf(
        "permission_revoked",
        "bluetooth_off",
        "advertise_failed",
        "scan_failed",
        "tokens_exhausted",
    )

    @Test
    fun `vocabulary is the spec words plus the frozen FE contract word`() {
        assertEquals(specVocabulary + "bluetooth_unsupported", PROXIMITY_ERRORS)
    }

    @Test
    fun `every spec word is accepted`() {
        for (word in specVocabulary) assertTrue(word, isValidProximityError(word))
    }

    @Test
    fun `bluetooth_unsupported stays accepted for the FE contract`() {
        assertTrue(isValidProximityError("bluetooth_unsupported"))
    }

    @Test
    fun `null is accepted`() {
        assertTrue(isValidProximityError(null))
    }

    @Test
    fun `phase B session_expired is rejected`() {
        assertFalse(isValidProximityError("session_expired"))
    }

    @Test
    fun `typos and unknown words are rejected`() {
        assertFalse(isValidProximityError("bluetooth_of"))
        assertFalse(isValidProximityError("advertise_fail"))
        assertFalse(isValidProximityError("PERMISSION_REVOKED"))
        assertFalse(isValidProximityError(""))
    }

    @Test
    fun `lastError drops a word outside the vocabulary`() {
        ProximityState.lastError = "bogus_word"
        assertNull(ProximityState.lastError)
    }

    @Test
    fun `lastError keeps a spec word`() {
        ProximityState.lastError = "tokens_exhausted"
        assertEquals("tokens_exhausted", ProximityState.lastError)
        ProximityState.lastError = null
    }
}
