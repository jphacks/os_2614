package jp.i32.chain.photos

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test

class PhotoPermissionTest {
    @Test fun api33ImagesGranted() = assertEquals("granted", photoPermission(33, true, false, "denied"))

    @Test fun api34FullAccessIsGranted() = assertEquals("granted", photoPermission(34, true, true, "denied"))

    @Test fun api34PartialIsLimited() = assertEquals("limited", photoPermission(34, false, true, "prompt"))

    @Test fun api34NonePassesThroughFallback() {
        assertEquals("prompt", photoPermission(34, false, false, "prompt"))
        assertEquals("denied", photoPermission(34, false, false, "denied"))
    }

    @Test fun api33SelectedAloneIsNotLimited() = assertEquals("prompt", photoPermission(33, false, true, "prompt"))

    @Test fun api32UsesStorageState() {
        assertEquals("granted", photoPermission(32, false, false, "granted"))
        assertEquals("denied", photoPermission(32, false, false, "denied"))
    }

    @Test fun requestAliasesPerVersion() {
        assertArrayEquals(arrayOf("mediaImages", "mediaSelected"), requestAliases(34))
        assertArrayEquals(arrayOf("mediaImages"), requestAliases(33))
        assertArrayEquals(arrayOf("storage"), requestAliases(32))
    }
}
