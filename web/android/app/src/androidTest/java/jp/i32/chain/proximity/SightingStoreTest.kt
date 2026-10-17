package jp.i32.chain.proximity

import android.content.Context
import android.content.SharedPreferences
import androidx.room.Room
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.lang.reflect.Proxy
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * [SightingStore] と [ProximityState] の instrumented test。実ファイルの DB を使い、
 * プロセス再生成に相当する DB 再オープン後も集約が保持されること (= IS-1 の核) を検証する。
 * record/drain/pending が main thread から SQLite を触らない (F-02) ことと、load() の
 * フォールバック不変条件 (F-01)・throttle・routing (F-12) もここで確認する。
 */
@RunWith(AndroidJUnit4::class)
class SightingStoreTest {

    private val appContext: Context
        get() = InstrumentationRegistry.getInstrumentation().targetContext

    private val ttlMs = 1000L
    private val dbName = "chain_sightings_test_${System.nanoTime()}"

    /** 同じファイルに対する新しい RoomDatabase 実例を作る (メモリキャッシュではない)。 */
    private fun newDb(): SightingDatabase =
        Room.databaseBuilder(appContext, SightingDatabase::class.java, dbName).build()

    @Before
    fun setUp() {
        appContext.deleteDatabase(dbName)
        clearPrefs()
        resetProximityState()
    }

    @After
    fun tearDown() {
        resetProximityState()
        clearPrefs()
        appContext.deleteDatabase(dbName)
    }

    private fun clearPrefs() {
        appContext.getSharedPreferences(SecureStore.LEGACY_PREFS, Context.MODE_PRIVATE).edit().clear().commit()
        SecureStore.create(appContext).clear()
    }

    @Test
    fun threeRecordsMergeIntoOneAggregate() {
        val db = newDb()
        try {
            val store = SightingStore(appContext, ttlMs, db)
            store.record("tok", 100L, -70)
            store.record("tok", 200L, -60)
            store.record("tok", 150L, -80)
            store.awaitIdle()

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
        store1.awaitIdle()
        assertEquals(1, store1.pending(200L))

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
            store2.awaitIdle()
            val drained = store2.drain(200L)
            assertEquals(1, drained.size)
            val agg = drained.getValue("tok")
            assertEquals(100L, agg.firstMs)
            assertEquals(200L, agg.lastMs)
            assertEquals(-60, agg.maxRssi)
            assertEquals(3, agg.count)
            assertEquals(0, store2.pending(200L))
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
            store.awaitIdle()

            val drained = store.drain(1000L)
            assertEquals(1, drained.size)
            assertFalse(drained.containsKey("stale"))
            assertTrue(drained.containsKey("live"))
            assertEquals(0, store.pending(1000L))
        } finally {
            db.close()
        }
    }

    @Test
    fun ttlBoundaryIsDroppedAndOneMillisInsideIsKept() {
        val db = newDb()
        try {
            val store = SightingStore(appContext, ttlMs, db)
            store.record("boundary", 0L, -50)
            store.record("fresh", 1L, -50)
            store.awaitIdle()

            val drained = store.drain(1000L)
            assertFalse("entry at ttl boundary must be dropped", drained.containsKey("boundary"))
            assertTrue("entry one ms inside ttl must be kept", drained.containsKey("fresh"))
            assertEquals(1, drained.size)
            assertEquals(0, store.pending(1000L))
        } finally {
            db.close()
        }
    }

    @Test
    fun pendingExcludesTtlExpiredEntries() {
        val db = newDb()
        try {
            val store = SightingStore(appContext, ttlMs, db)
            store.record("stale", 0L, -50)
            store.record("live", 500L, -50)
            store.awaitIdle()
            assertEquals(1, store.pending(1000L))
        } finally {
            db.close()
        }
    }

    @Test
    fun recordPrunesExpiredRowsBeforeDrain() {
        val db = newDb()
        try {
            val store = SightingStore(appContext, ttlMs, db)
            store.record("stale", 0L, -50)
            // 5s (剪定間隔) 以上進んだ record が record 時の剪定を回す。
            store.record("fresh", 10_000L, -50)
            store.awaitIdle()

            assertEquals(1, store.pending(10_000L))
            val rows = runBlocking { db.sightingDao().getAll() }
            assertEquals(listOf("fresh"), rows.map { it.token })
        } finally {
            db.close()
        }
    }

    @Test
    fun concurrentRecordsMergeDeterministically() {
        val db = newDb()
        try {
            val store = SightingStore(appContext, ttlMs = 60_000L, database = db)
            val threads = 8
            val perThread = 100
            val pool = Executors.newFixedThreadPool(threads)
            val start = CountDownLatch(1)
            val done = CountDownLatch(threads)
            try {
                repeat(threads) { t ->
                    pool.execute {
                        start.await()
                        repeat(perThread) { i ->
                            store.record("tok", (t * perThread + i).toLong(), -100 + i)
                        }
                        done.countDown()
                    }
                }
                start.countDown()
                assertTrue(done.await(10, TimeUnit.SECONDS))
            } finally {
                pool.shutdown()
            }
            store.awaitIdle()

            assertEquals(1, store.pending((threads * perThread).toLong()))
            val agg = store.drain(60_000L).getValue("tok")
            assertEquals(threads * perThread, agg.count)
            assertEquals(0L, agg.firstMs)
            assertEquals((threads * perThread - 1).toLong(), agg.lastMs)
            assertEquals(-100 + perThread - 1, agg.maxRssi)
        } finally {
            db.close()
        }
    }

    @Test
    fun recordIsNonBlockingOnTheMainThread() {
        val db = newDb()
        try {
            val store = SightingStore(appContext, ttlMs, db)
            store.awaitIdle()
            InstrumentationRegistry.getInstrumentation().runOnMainSync {
                store.record("tok", 100L, -70)
                store.record("tok", 200L, -60)
                store.pending(200L)
            }
            store.awaitIdle()
            val drained = store.drain(200L)
            assertEquals(1, drained.size)
            assertEquals(2, drained.getValue("tok").count)
        } finally {
            db.close()
        }
    }

    // ---- ProximityState wiring (F-01 / F-12) ----

    @Test
    fun loadFallsBackToDefaultsWhenSecureReadsThrow() {
        ProximityState.load(appContext, secureFactory = { brokenSecureStore() }, storeFactory = { null })

        assertEquals(900L, ProximityState.windowSec)
        assertEquals(0L, ProximityState.clockOffsetMs)
        assertTrue(ProximityState.tokens.isEmpty())

        // in-memory フォールバック (store == null) でも record -> drain が回る。
        ProximityState.recordSighting(byteArrayOf(1, 2, 3), -50)
        assertEquals(1, ProximityState.drainSightings().length())
    }

    @Test
    fun recordSightingRoutesToRoomAndThrottlesEvents() {
        val db = newDb()
        try {
            val store = SightingStore(appContext, database = db)
            ProximityState.load(appContext, storeFactory = { store })

            val events = mutableListOf<String>()
            ProximityState.eventSink = { event, _ -> if (event == "sighting") events.add(event) }

            val token = byteArrayOf(9, 9, 9)
            ProximityState.recordSighting(token, -50)
            ProximityState.recordSighting(token, -55)
            assertEquals(1, events.size)

            // 実時間を待たずに 5 秒窓を越えさせる (clock offset を進める)。
            ProximityState.setClockOffset(appContext, 10_000L)
            ProximityState.recordSighting(token, -60)
            assertEquals(2, events.size)

            store.awaitIdle()
            val drained = ProximityState.drainSightings()
            assertEquals(1, drained.length())
            assertEquals(3, drained.getJSONObject(0).getInt("count"))
        } finally {
            db.close()
        }
    }

    private fun brokenSecureStore(): SecureStore {
        val prefs = Proxy.newProxyInstance(
            SharedPreferences::class.java.classLoader,
            arrayOf(SharedPreferences::class.java),
        ) { _, method, _ ->
            when (method.name) {
                "getLong", "getString" -> throw SecurityException("forced decrypt failure")
                else -> defaultValue(method.returnType)
            }
        } as SharedPreferences
        return SecureStore::class.java
            .getDeclaredConstructor(SharedPreferences::class.java, String::class.java)
            .apply { isAccessible = true }
            .newInstance(prefs, "forced decrypt failure") as SecureStore
    }

    private fun defaultValue(type: Class<*>): Any? = when {
        type == Boolean::class.javaPrimitiveType -> false
        type == Long::class.javaPrimitiveType -> 0L
        type == Int::class.javaPrimitiveType -> 0
        type == Float::class.javaPrimitiveType -> 0f
        type == Map::class.java -> emptyMap<String, Any>()
        else -> null
    }

    private fun resetProximityState() {
        ProximityState.eventSink = null
        ProximityState.onBeaconConfigChanged = null
        ProximityState.lastError = null
        setField("loaded", false)
        setField("store", null)
        setField("secure", null)
        setField("windowSec", 900L)
        setField("clockOffsetMs", 0L)
        setField("tokens", emptyList<ProximityState.Token>())
        (getField("lastSightingEvent") as ConcurrentHashMap<*, *>).clear()
        (getField("sightings") as SightingAggregator).drain(Long.MAX_VALUE)
    }

    private fun setField(name: String, value: Any?) {
        ProximityState::class.java.getDeclaredField(name).apply {
            isAccessible = true
            set(ProximityState, value)
        }
    }

    private fun getField(name: String): Any? =
        ProximityState::class.java.getDeclaredField(name).apply { isAccessible = true }.get(ProximityState)
}
