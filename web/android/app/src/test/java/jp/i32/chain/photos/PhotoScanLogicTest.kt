package jp.i32.chain.photos

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PhotoScanLogicTest {
    @Test fun takenAtUsesDateTakenWhenPositive() = assertEquals(5_000L, takenAtMillis(5_000L, 9L))

    @Test fun takenAtFallsBackToDateAdded() {
        assertEquals(9_000L, takenAtMillis(0L, 9L))
        assertEquals(9_000L, takenAtMillis(-1L, 9L))
    }

    @Test fun cameraFolderOnQAndAbove() {
        assertTrue(isInCameraFolder(29, "DCIM/Camera/", null))
        assertTrue(isInCameraFolder(34, "DCIM/Camera/2026/", null))
        assertFalse(isInCameraFolder(31, "Pictures/CHAIN/party/", null))
        assertFalse(isInCameraFolder(31, "DCIM/Camera", null))
        assertFalse(isInCameraFolder(31, null, "/storage/emulated/0/DCIM/Camera/a.jpg"))
    }

    @Test fun cameraFolderBelowQ() {
        assertTrue(isInCameraFolder(28, null, "/storage/emulated/0/DCIM/Camera/a.jpg"))
        assertFalse(isInCameraFolder(28, null, "/storage/emulated/0/Pictures/a.jpg"))
        assertFalse(isInCameraFolder(28, "DCIM/Camera/", null))
        assertFalse(isInCameraFolder(28, null, null))
    }

    @Test fun windowIsInclusive() {
        assertTrue(isWithinWindow(100, 100, 200))
        assertTrue(isWithinWindow(200, 100, 200))
        assertFalse(isWithinWindow(99, 100, 200))
        assertFalse(isWithinWindow(201, 100, 200))
    }

    @Test fun sinceFloorSubtractsMarginAndClamps() {
        assertEquals(940L, sinceFloorSeconds(1_000_000L))
        assertEquals(0L, sinceFloorSeconds(10_000L))
        assertEquals(0L, sinceFloorSeconds(-5L))
    }

    @Test fun debounceDeadlineAddsDelay() {
        assertEquals(11_500L, debounceDeadline(10_000L))
        assertEquals(10_200L, debounceDeadline(10_000L, 200L))
    }

    @Test fun rescanLowerBoundRewindsAndClampsAtSince() {
        assertEquals(880L, rescanLowerBoundSeconds(1_000L, 500L))
        assertEquals(500L, rescanLowerBoundSeconds(550L, 500L))
        assertEquals(500L, rescanLowerBoundSeconds(500L, 500L))
        assertEquals(500L, rescanLowerBoundSeconds(0L, 500L))
    }

    @Test fun cursorOrdering() {
        assertTrue(isAfterCursor(11, 1, 10, 99))
        assertTrue(isAfterCursor(10, 6, 10, 5))
        assertFalse(isAfterCursor(10, 5, 10, 5))
        assertFalse(isAfterCursor(9, 100, 10, 5))
    }

    @Test fun extensionPerMime() {
        assertEquals("png", extensionForMime("image/png"))
        assertEquals("webp", extensionForMime("image/webp"))
        assertEquals("jpg", extensionForMime("image/jpeg"))
        assertEquals("jpg", extensionForMime("image/heic"))
    }

    @Test fun staleTempFiles() {
        val day = 24 * 60 * 60 * 1000L
        assertTrue(isStaleTempFile(0L, day + 1))
        assertFalse(isStaleTempFile(0L, day))
    }

    @Test fun releasablePathChecks() {
        val base = File(System.getProperty("java.io.tmpdir"), "chain-photos-test-${System.nanoTime()}/chain-photos")
        base.mkdirs()
        try {
            assertTrue(isReleasablePath(base, File(base, "1.jpg").path))
            assertFalse(isReleasablePath(base, base.path))
            assertFalse(isReleasablePath(base, ""))
            assertFalse(isReleasablePath(base, File(base, "../secret.jpg").path))
            assertFalse(isReleasablePath(base, File(base.parentFile, "chain-photos-evil/1.jpg").path))
            assertFalse(isReleasablePath(base, "/etc/hosts"))
        } finally {
            base.parentFile.deleteRecursively()
        }
    }

    @Test fun recentUrisExpireAndCap() {
        val set = RecentUriSet(max = 3, ttlMs = 1_000L)
        set.add("a", 0L)
        assertTrue(set.contains("a", 500L))
        assertFalse(set.contains("a", 1_500L))
        set.add("1", 2_000L)
        set.add("2", 2_001L)
        set.add("3", 2_002L)
        set.add("4", 2_003L)
        assertFalse(set.contains("1", 2_004L))
        assertTrue(set.contains("4", 2_004L))
    }
}
