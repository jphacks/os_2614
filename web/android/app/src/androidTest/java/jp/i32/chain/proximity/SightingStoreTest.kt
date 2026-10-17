package jp.i32.chain.proximity

import android.content.Context
import androidx.room.Room
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * [SightingStore] の instrumented test。実ファイルの DB を使い、プロセス再生成に相当する
 * DB 再オープン後も集約が保持されること (= IS-1 の核) を検証する。
 */
@RunWith(AndroidJUnit4::class)
class SightingStoreTest {

    private val appContext: Context
        get() = InstrumentationRegistry.getInstrumentation().targetContext

    private val ttlMs = 1000L
    private val dbName = "chain_sightings_test"

    /** 同じファイルに対する新しい RoomDatabase 実例を作る (メモリキャッシュではない)。 */
    private fun newDb(): SightingDatabase =
        Room.databaseBuilder(appContext, SightingDatabase::class.java, dbName).build()

    @Before
    fun setUp() {
        appContext.deleteDatabase(dbName)
    }

    @After
    fun tearDown() {
        appContext.deleteDatabase(dbName)
    }

    @Test
    fun threeRecordsMergeIntoOneAggregate() {
        val db = newDb()
        try {
            val store = SightingStore(appContext, ttlMs, db)
            store.record("tok", 100L, -70)
            store.record("tok", 200L, -60)
            store.record("tok", 150L, -80)

            val drained = store.drain(200L)
            assertEquals(1, drained.size)
            val agg = drained.getValue("tok")
            assertEquals(100L, agg.firstMs)
            assertEquals(200L, agg.lastMs)
            assertEquals(-60, agg.maxRssi)
            assertEquals(3, agg.count)
        } finally {
            db.close()
        }
    }

    @Test
    fun aggregateSurvivesDatabaseReopen() {
        val first = newDb()
        val store1 = SightingStore(appContext, ttlMs, first)
        store1.record("tok", 100L, -70)
        store1.record("tok", 200L, -60)
        store1.record("tok", 150L, -80)
        assertEquals(1, store1.pending())

        // プロセス死相当: 1 つ目の DB 実例を閉じ、以降はディスクからのみ読める状態にする。
        first.close()
        assertTrue(
            "DB file must persist on disk after close",
            appContext.getDatabasePath(dbName).exists(),
        )

        // 同じファイルへの新しい RoomDatabase 実例 (= 新しい SightingStore)。
        // この実例は record を一切見ていないため、返る集約はディスクから読まれたもの。
        val second = newDb()
        try {
            val store2 = SightingStore(appContext, ttlMs, second)
            val drained = store2.drain(200L)
            assertEquals(1, drained.size)
            val agg = drained.getValue("tok")
            assertEquals(100L, agg.firstMs)
            assertEquals(200L, agg.lastMs)
            assertEquals(-60, agg.maxRssi)
            assertEquals(3, agg.count)
            assertEquals(0, store2.pending())
        } finally {
            second.close()
        }
    }

    @Test
    fun drainRemovesReturnedAndExpiredRows() {
        val db = newDb()
        try {
            val store = SightingStore(appContext, ttlMs, db)
            store.record("stale", 0L, -50)
            store.record("live", 500L, -50)

            val drained = store.drain(1000L)
            assertEquals(1, drained.size)
            assertFalse(drained.containsKey("stale"))
            assertTrue(drained.containsKey("live"))
            assertEquals(0, store.pending())
        } finally {
            db.close()
        }
    }

    @Test
    fun ttlBoundaryIsDroppedAndOneMillisInsideIsKept() {
        val db = newDb()
        try {
            val store = SightingStore(appContext, ttlMs, db)
            store.record("boundary", 0L, -50) // lastMs == nowMs - ttlMs
            store.record("fresh", 1L, -50) // lastMs == nowMs - ttlMs + 1

            val drained = store.drain(1000L)
            assertFalse("entry at ttl boundary must be dropped", drained.containsKey("boundary"))
            assertTrue("entry one ms inside ttl must be kept", drained.containsKey("fresh"))
            assertEquals(1, drained.size)
            assertEquals(0, store.pending())
        } finally {
            db.close()
        }
    }

    /** todo 9 が main thread から呼ぶため、main thread 呼び出しで Room が例外を投げないことを証明する。 */
    @Test
    fun publicMethodsAreCallableFromMainThread() {
        val db = newDb()
        try {
            val store = SightingStore(appContext, ttlMs, db)
            InstrumentationRegistry.getInstrumentation().runOnMainSync {
                store.record("tok", 100L, -70)
                store.record("tok", 200L, -60)
                assertEquals(1, store.pending())
                val drained = store.drain(200L)
                assertEquals(1, drained.size)
                assertEquals(2, drained.getValue("tok").count)
                assertEquals(0, store.pending())
            }
        } finally {
            db.close()
        }
    }
}
