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
import kotlin.coroutines.CoroutineContext
import kotlinx.coroutines.CoroutineDispatcher
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

    /** この test が作った GatedDispatcher。@After で executor を閉じる。 */
    private val dispatchers = mutableListOf<GatedDispatcher>()

    private fun gatedDispatcher(): GatedDispatcher = GatedDispatcher().also { dispatchers += it }

    /** 同じファイルに対する新しい RoomDatabase 実例を作る (メモリキャッシュではない)。 */
    private fun newDb(): SightingDatabase =
        Room.databaseBuilder(appContext, SightingDatabase::class.java, dbName).build()

    @Before
    fun setUp() {
        dispatchers.clear()
        appContext.deleteDatabase(dbName)
        clearPrefs()
        resetProximityState()
    }

    @After
    fun tearDown() {
        dispatchers.forEach { it.close() }
        dispatchers.clear()
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

    // ---- F-02b/F-02c: drain must delete only the tokens it returned, with a synchronous mirror ----

    @Test
    fun drainDeletesOnlyReturnedTokensFromTheDatabase() {
        val db = newDb()
        try {
            val store = SightingStore(appContext, ttlMs, db)
            store.awaitIdle() // hydration completes against the empty DB first
            // A record merges into the mirror synchronously, so the following drain returns it.
            store.record("returned", 100L, -50)
            // A committed row whose token is NOT in the mirror snapshot (out-of-band/legacy row):
            // the scoped delete must not touch it.
            runBlocking { db.sightingDao().upsert(SightingEntity("unreturned", 120L, 120L, -40, 1)) }

            val drained = store.drain(200L)
            assertEquals(setOf("returned"), drained.keys)
            store.awaitIdle()

            val tokens = runBlocking { db.sightingDao().getAll() }.map { it.token }.toSet()
            assertEquals("the drain must delete only the tokens it returned", setOf("unreturned"), tokens)
        } finally {
            db.close()
        }
    }

    @Test
    fun sameTokenRecordBeforeDrainSnapshotIsReturned() {
        val db = newDb()
        try {
            val dispatcher = gatedDispatcher()
            val store = SightingStore(appContext, ttlMs, db, dispatcher)

            // 1st observation completes fully (mirror entry + committed DB row).
            store.record("tok", 100L, -50)
            dispatcher.resume()
            store.awaitIdle()
            assertEquals(1L, runBlocking { db.sightingDao().count().toLong() })

            // 2nd observation for the SAME token, enqueued but held by the gate.
            dispatcher.pause()
            store.record("tok", 200L, -60)

            // drain while the 2nd record's persist job is still gated. Because the merge is synchronous,
            // the 2nd observation is already in the mirror, so the snapshot MUST include it.
            val drained = store.drain(200L)
            assertEquals(setOf("tok"), drained.keys)
            val agg = drained.getValue("tok")
            assertEquals("the returned aggregate must reflect the 2nd observation", 2, agg.count)
            assertEquals(200L, agg.lastMs)
            assertEquals(-50, agg.maxRssi)

            dispatcher.resume()
            store.awaitIdle()

            // loss-free: the returned token's row is gone (it was returned) and no live mirror entry remains.
            assertEquals(0L, runBlocking { db.sightingDao().count().toLong() })
            assertEquals(0, store.pending(200L))
        } finally {
            db.close()
        }
    }

    @Test
    fun recordAfterDrainSnapshotSurvivesTheDelete() {
        val db = newDb()
        try {
            val dispatcher = gatedDispatcher()
            val store = SightingStore(appContext, ttlMs, db, dispatcher)

            store.record("tok", 100L, -50)
            dispatcher.resume()
            store.awaitIdle()

            dispatcher.pause()
            // drain FIRST: returns the 1st observation and enqueues the scoped delete.
            val drained = store.drain(100L)
            assertEquals(setOf("tok"), drained.keys)
            assertEquals(1, drained.getValue("tok").count)

            // 2nd observation AFTER the snapshot: its merge is synchronous and its upsert job is chained
            // after the delete job, so its row must SURVIVE.
            store.record("tok", 200L, -60)
            // The synchronous merge must already be visible while the persist job is still gated.
            assertEquals(1, store.pending(200L))

            dispatcher.resume()
            store.awaitIdle()

            val rows = runBlocking { db.sightingDao().getAll() }
            assertEquals(listOf("tok"), rows.map { it.token })
            assertEquals(200L, rows[0].lastMs)
            assertEquals(1, rows[0].count)
        } finally {
            db.close()
        }
    }

    @Test
    fun drainBeforeHydrationDeletesNothingAndRowsSurvive() {
        val db = newDb()
        try {
            runBlocking { db.sightingDao().upsert(SightingEntity("persisted", 100L, 100L, -40, 1)) }
            val dispatcher = gatedDispatcher()
            val store = SightingStore(appContext, ttlMs, db, dispatcher)

            // hydration has not run, so the mirror is empty and this drain returns nothing.
            val beforeHydration = store.drain(200L)
            assertTrue("a pre-hydration drain returns nothing", beforeHydration.isEmpty())

            dispatcher.resume()
            store.awaitIdle()
            assertEquals(
                "pre-hydration drain must not delete persisted rows",
                1L,
                runBlocking { db.sightingDao().count().toLong() },
            )

            val afterHydration = store.drain(200L)
            assertEquals(setOf("persisted"), afterHydration.keys)
        } finally {
            db.close()
        }
    }

    @Test
    fun recordReturnsWithoutTouchingTheDao() {
        val db = newDb()
        try {
            val dispatcher = gatedDispatcher()
            val store = SightingStore(appContext, ttlMs, db, dispatcher)
            dispatcher.resume()
            store.awaitIdle()
            dispatcher.pause()

            store.record("tok", 100L, -50)
            assertEquals(
                "record must not touch the DAO synchronously",
                0L,
                runBlocking { db.sightingDao().count().toLong() },
            )

            dispatcher.resume()
            store.awaitIdle()
            assertEquals(
                "the chained job must persist the record",
                1L,
                runBlocking { db.sightingDao().count().toLong() },
            )
        } finally {
            db.close()
        }
    }

    @Test
    fun hydrationCombinesPersistedAndIncomingSameTokenObservation() {
        val db = newDb()
        try {
            // 再起動前の永続行 (count=5)。
            runBlocking {
                db.sightingDao().upsert(Aggregate(1000L, 2000L, -40, 5).toEntity("A"))
            }
            val dispatcher = gatedDispatcher()
            val store = SightingStore(appContext, ttlMs = 60_000L, database = db, dispatcher = dispatcher)

            // hydration が gated の間に同じ token を record する (ミラーはまだ空)。
            store.record("A", 3000L, -50)

            dispatcher.resume()
            store.awaitIdle()

            // hydrate は永続行を skip/overwrite せず combine し、record の upsert は実行時に
            // ミラーの現在値 (combined) を書く。
            val row = runBlocking { db.sightingDao().getAll() }.single { it.token == "A" }
            assertEquals("hydrate must combine the persisted count", 6, row.count)
            assertEquals(1000L, row.firstMs)
            assertEquals(3000L, row.lastMs)
            assertEquals(-40, row.maxRssi)
            assertEquals(1, store.pending(3000L))
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
            ProximityState.setClockOffset(10_000L)
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

/**
 * テスト用ディスパッチャ。既定は closed で、dispatch された Runnable を単一スレッドの executor 上で
 * gate の前で止める。[resume] するまで何も実行しないため、record と drain の順序を sleep 無しで
 * 決定的に組み立てられる (record の chained job を「まだ走っていない」状態に固定できる)。
 */
private class GatedDispatcher : CoroutineDispatcher() {
    private val executor = Executors.newSingleThreadExecutor { r ->
        Thread(r, "gated-io").apply { isDaemon = true }
    }
    private val gate = Object()
    private var open = false

    override fun dispatch(context: CoroutineContext, block: Runnable) {
        executor.execute {
            synchronized(gate) { while (!open) gate.wait() }
            block.run()
        }
    }

    fun pause() = synchronized(gate) { open = false }

    fun resume() = synchronized(gate) {
        open = true
        gate.notifyAll()
    }

    fun close() {
        resume()
        executor.shutdown()
    }
}
