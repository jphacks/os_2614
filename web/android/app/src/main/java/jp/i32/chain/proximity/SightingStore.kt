package jp.i32.chain.proximity

import android.content.Context
import android.util.Log
import kotlinx.coroutines.CoroutineDispatcher
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
 *  - record: first=min / last=max / maxRssi=max / count+1 で 1 件にマージ
 *  - drain: `lastMs <= nowMs - ttlMs` は返さずに捨て、返した token だけを DB から消す
 *
 * スレッド方針 (F-02): main thread では一切 SQLite I/O をしない。
 *  - [mirror] が読み取り・書き込みの権威状態。record のマージは呼び出しスレッドで同期に行い
 *    (HashMap 操作のみ、ディスク I/O なし、μs)、DB への upsert だけを [scope] へ非同期に投げる。
 *  - 読み取り ([pending] / [drain]) も [mirror] から返す。
 *
 * ミラーと DB の整合 (F-02c: 同期ミラー):
 *  - [mirror] は同期の権威状態。record が返った時点で必ずミラーに反映されているので、drain の
 *    snapshot は「既に record が返った」観測を取りこぼさない。旧実装はマージを chained job 内で
 *    行っていたため snapshot と record job の順序が定まらず、committed 行を消す損失パスがあった。
 *  - drain の後に来た record は snapshot に無いので削除対象にならず、その upsert job は delete job
 *    の後に連結されるため消されない。
 *  - hydration は combine: 既にミラーにある entry (このプロセスの load 後に record された観測) と
 *    永続行 (再起動前の観測) を first=min / last=max / maxRssi=max / count=和 で合成する。skip も
 *    overwrite もしない (どちらも観測を失う)。
 *  - record の upsert は「実行時に読んだミラーの現在値」を書く (capture しない)。hydrate の combine を
 *    反映し、drain 済みの entry は書かない (返済済み集約の復活を防ぐ)。
 *  - record→upsert と drain→(token 削除 + 期限切れ掃除) は [scope] 上で前の操作に join して直列化する。
 *
 * 耐久性の窓 (F-02 の durability window):
 *  - record は同期マージの後、DB upsert を enqueue して即座に返る。upsert が commit した時点で
 *    ディスクに載り、以降の SIGKILL / process death / 再起動をまたいで残る。
 *  - 窓 = record() が返ってから upsert が commit するまで (通常 μs〜ms)。この間にプロセスが死ぬと
 *    その 1 件はミラーごと失われる。ミラーは同期なので、drain はこの窓の集約も正しく返せる。
 */
class SightingStore(
    context: Context,
    private val ttlMs: Long = DEFAULT_TTL_MS,
    private val database: SightingDatabase = SightingDatabase.get(context),
    dispatcher: CoroutineDispatcher = Dispatchers.IO,
) {
    private val dao = database.sightingDao()

    /** 読み取りのソース。ディスクではなくここから返すため main thread でも即答できる。 */
    private val lock = Any()
    private val mirror = HashMap<String, Aggregate>()

    private val scope = CoroutineScope(SupervisorJob() + dispatcher)
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

    /** 破棄されたプロセスの DB 行をミラーへ読み込む。既存のミラー entry とは combine する。 */
    private suspend fun hydrate() {
        val rows = runCatching { dao.getAll() }.getOrElse { e ->
            Log.w(TAG, "sighting hydration failed", e)
            return
        }
        synchronized(lock) {
            for (row in rows) {
                val persisted = row.toAggregate()
                val incoming = mirror[row.token]
                // combine (skip も overwrite もしない): ミラーの entry にはこのプロセスの load 後に
                // record された観測が入っている。count を足すのは hydrate が最初の enqueued job で
                // record の upsert がまだ書かれておらず、両者の観測に重複が無いため (F2d (A)4)。
                mirror[row.token] = if (incoming == null) persisted else combine(persisted, incoming)
            }
        }
    }

    /**
     * first=min / last=max / maxRssi=max / count+1 でミラーを同期マージし、DB 書き込みを enqueue する。
     * マージは呼び出しスレッドで [lock] の下に同期実行する (HashMap 操作のみ、ディスク I/O なし)。
     * これにより drain の snapshot は「既に record が返った」観測を取りこぼさない。
     * upsert する値は [record] 呼び出し時の capture ではなく「実行時に読んだミラーの現在値」なので、
     * hydrate の combine や後続の観測を反映した latest-wins (冪等) になる。
     */
    fun record(token: String, nowMs: Long, rssi: Int) {
        val prune = synchronized(lock) {
            val shouldPrune = shouldPruneLocked(nowMs)
            if (shouldPrune) pruneLocked(nowMs)
            mirror[token] = merge(mirror[token], nowMs, rssi)
            shouldPrune
        }
        enqueue {
            runCatching {
                // 実行時にミラーの現在値を読む。entry が既に無ければ (drain が返した後) upsert しない
                // = 返済済みの集約を復活させない (F2d (E)2 の重複再送も同時に解消)。
                val current = synchronized(lock) { mirror[token] }
                if (current != null) dao.upsert(current.toEntity(token))
                // F-04: 期限切れ行を record 時にも落とす (drain まで放置しない)。
                if (prune) dao.deleteExpired(nowMs - ttlMs)
            }.onFailure { Log.w(TAG, "sighting record failed", it) }
        }
    }

    /**
     * [nowMs] 時点で生きている entry を返し、ミラーから消す。
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
        // 返した token だけを DB から消す (full-table delete は IS-1 違反になるため禁止)。
        //  - ミラーは同期 merge なので snapshot は「record が返った」観測を必ず含む。snapshot に
        //    無い record (drain の後に来たもの) は削除対象にならず、その upsert は delete の後に連結。
        //  - hydration 前はミラーが空 = snapshot が空なので何も enqueue しない (下の early return)。
        //    永続化済みの行はそのまま残り、hydration 後の drain が同じように返す。
        if (live.isEmpty()) return live
        val tokens = live.keys.toList()
        val cutoff = nowMs - ttlMs
        enqueue {
            runCatching {
                // SQLite の bind 変数上限 (旧 999) を超えないようチャンクして削除する。
                // 空リストは上の `if (live.isEmpty()) return` で既に除外済み (IN () を作らない)。
                for (chunk in tokens.chunked(DELETE_CHUNK_SIZE)) dao.deleteByTokens(chunk)
                // 期限切れ行も drain 時に掃除する (record 時の剪定は 5s throttle のため)。
                dao.deleteExpired(cutoff)
            }.onFailure { Log.w(TAG, "sighting drain cleanup failed", it) }
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

        /** deleteByTokens の 1 回あたりの token 数。旧 SQLite の bind 変数上限 (999) を下回る値。 */
        private const val DELETE_CHUNK_SIZE = 500
        private const val TAG = "ChainProximity"
    }
}
