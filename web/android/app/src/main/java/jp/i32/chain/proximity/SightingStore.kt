package jp.i32.chain.proximity

import android.content.Context
import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Room 版の sightings ストア。集約を SQLite に永続化し、プロセス死・OS kill・端末再起動を
 * またいでも失われないようにする。セマンティクスは純粋な [SightingAggregator] と同一:
 *  - record: first=min / last=max / maxRssi=max / count+1 で 1 行をマージ
 *  - drain: `lastMs <= nowMs - ttlMs` を送らずに捨て、残りを返したうえで全体を消す
 *
 * スレッド方針 (F-02): main thread では一切 SQLite I/O をしない。
 *  - 読み取り ([pending] / [drain]) は in-memory の [mirror] から返す。ディスクに触れない。
 *  - 書き込み ([record]) と hydration は [scope] へ非同期に投げ、呼び出し側をブロックしない。
 *    DAO はすべて suspend で、前の操作の完了に連結 (chain) して直列化する。
 *
 * ミラーと DB の整合 (順序の明記):
 *  - [mirror] が読み取りの唯一のソース。全ての変更 (hydration → record → ミラー剪定) は
 *    前の操作の完了に join してから走るため、必ず hydration の後に record が適用される。
 *  - hydration は最初の操作。既存 DB 行をミラーに読み込み、以降の record と競合しない。
 *  - drain はミラーから生きた行を snapshot して即座に全消しし、DB 側の削除は chain の末尾に投げる。
 *
 * 耐久性の窓 (F-02 の durability window):
 *  - record は「ミラーへのマージ + DB upsert」を enqueue して即座に返る。
 *    この upsert トランザクションが commit した時点でディスクに載り、以降の SIGKILL /
 *    process death / 再起動をまたいで残る (Room の durable ストアが唯一の真実)。
 *  - 窓 = record() が返ってから上の upsert が commit するまで。通常はミリ秒未満だが、
 *    この間にプロセスが死ぬとその 1 件はミラーごと失われる。drain の DB 削除も同様の窓を持つ。
 */
class SightingStore(
    context: Context,
    private val ttlMs: Long = DEFAULT_TTL_MS,
    private val database: SightingDatabase = SightingDatabase.get(context),
) {
    private val dao = database.sightingDao()

    /** 読み取りのソース。ディスクではなくここから返すため main thread でも即答できる。 */
    private val lock = Any()
    private val mirror = HashMap<String, Aggregate>()

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val chainLock = Any()

    /** 直近に enqueue した操作。次の操作はこれに join してから走る (直列化 + 完了観測)。 */
    private var lastJob: Job? = null

    private var lastPruneMs: Long? = null

    init {
        enqueue { hydrate() }
    }

    /** [block] を直前の操作の完了後に走らせる。どのスレッドから呼んでもよい。 */
    private fun enqueue(block: suspend () -> Unit) {
        synchronized(chainLock) {
            val previous = lastJob
            lastJob = scope.launch {
                previous?.join()
                block()
            }
        }
    }

    /** 破棄されたプロセスの DB 行をミラーへ読み込む。 */
    private suspend fun hydrate() {
        val rows = runCatching { dao.getAll() }.getOrElse { e ->
            Log.w(TAG, "sighting hydration failed", e)
            return
        }
        synchronized(lock) {
            for (row in rows) mirror[row.token] = row.toAggregate()
        }
    }

    /**
     * first=min / last=max / maxRssi=max / count+1 でミラーをマージし、その行を DB へ upsert する。
     * main thread から呼んでもディスクに触れない (enqueue のみ)。永続化の窓はクラス doc を参照。
     */
    fun record(token: String, nowMs: Long, rssi: Int) {
        enqueue {
            runCatching {
                var prune = false
                val merged = synchronized(lock) {
                    prune = shouldPruneLocked(nowMs)
                    if (prune) pruneLocked(nowMs)
                    val old = mirror[token]
                    merge(old, nowMs, rssi).also { mirror[token] = it }
                }
                dao.upsert(merged.toEntity(token))
                // F-04: 期限切れ行を record 時にも落とす (drain まで放置しない)。
                if (prune) dao.deleteExpired(nowMs - ttlMs)
            }.onFailure { Log.w(TAG, "sighting record failed", it) }
        }
    }

    /**
     * [nowMs] 時点で生きている entry を返し、ミラーを全消しする。
     * `lastMs <= nowMs - ttlMs` の entry は返さずに捨てる (境界の等値は捨てる)。
     * 呼び出しスレッドではミラーの snapshot のみで、DB の後片付けは chain の末尾へ投げる。
     */
    fun drain(nowMs: Long): Map<String, Aggregate> {
        val live = synchronized(lock) {
            val cutoff = nowMs - ttlMs
            val result = HashMap<String, Aggregate>()
            val iterator = mirror.entries.iterator()
            while (iterator.hasNext()) {
                val entry = iterator.next()
                if (entry.value.lastMs > cutoff) result[entry.key] = entry.value
                iterator.remove()
            }
            result
        }
        enqueue {
            runCatching { dao.deleteAll() }
                .onFailure { Log.w(TAG, "sighting drain cleanup failed", it) }
        }
        return live
    }

    /** [nowMs] 時点で生きた行数。TTL 切れ (F-04) は数えない。 */
    fun pending(nowMs: Long): Int = synchronized(lock) {
        val cutoff = nowMs - ttlMs
        mirror.values.count { it.lastMs > cutoff }
    }

    /**
     * enqueue 済みの操作がすべて完了するまで待つ。テストが「record した集約がディスクにある」
     * ことを sleep 無しで決定的に確かめるための seam。呼び出しスレッドはブロックされる。
     */
    internal fun awaitIdle(timeoutMs: Long = 10_000L) {
        val job = synchronized(chainLock) { lastJob }
        if (job == null || job.isCompleted) return
        val latch = CountDownLatch(1)
        job.invokeOnCompletion { latch.countDown() }
        check(latch.await(timeoutMs, TimeUnit.MILLISECONDS)) { "sighting worker did not drain in ${timeoutMs}ms" }
    }

    private fun shouldPruneLocked(nowMs: Long): Boolean {
        val last = lastPruneMs ?: return true
        return nowMs - last >= PRUNE_INTERVAL_MS
    }

    /** 期限切れ entry をミラーから落とす。lock 保持下で呼ぶ。 */
    private fun pruneLocked(nowMs: Long) {
        lastPruneMs = nowMs
        val cutoff = nowMs - ttlMs
        mirror.entries.removeIf { it.value.lastMs <= cutoff }
    }

    private fun merge(old: Aggregate?, nowMs: Long, rssi: Int): Aggregate =
        if (old == null) {
            Aggregate(nowMs, nowMs, rssi, 1)
        } else {
            Aggregate(
                minOf(old.firstMs, nowMs),
                maxOf(old.lastMs, nowMs),
                maxOf(old.maxRssi, rssi),
                old.count + 1,
            )
        }

    companion object {
        const val DEFAULT_TTL_MS = 24L * 60 * 60 * 1000

        /** record 時の剪定を回す最短間隔 (これを超えない頻度では DB/ミラーを舐めない)。 */
        private const val PRUNE_INTERVAL_MS = 5_000L
        private const val TAG = "ChainProximity"
    }
}
