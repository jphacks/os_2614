package jp.i32.chain.proximity

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RevocationPolicyTest {

    @Test
    fun `keeps persisted intent when the permission is still granted`() {
        assertFalse(RevocationPolicy.shouldClearPersistedIntent(permissionGranted = true))
    }

    @Test
    fun `clears persisted intent on an actual revocation`() {
        assertTrue(RevocationPolicy.shouldClearPersistedIntent(permissionGranted = false))
    }
}
