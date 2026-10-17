package jp.i32.chain.proximity

import android.content.Context
import androidx.room.withTransaction
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.runBlocking

/**
 * Room 版の sightings ストア。集約を SQLite に永続化し、プロセス死・OS kill・端末再起動を
 * またいでも失われないようにする。セマンティクスは純粋な [SightingAggregator] と同一:
 *  - record: first=min / last=max / maxRssi=max / count+1 で 1 行をマージ
 *  - drain: `lastMs <= nowMs - ttlMs` を送らずに捨て、残りを返したうえで全体を消す
 *
 * スレッド安全: DAO は suspend で、すべての呼び出しを内部の IO ディスパッチャで実行する。
 * したがって main thread からそのまま呼んでも Room の main-thread ガードには触れない
 * (main-thread query の許可設定には頼らない)。
 */
class SightingStore(
    context: Context,
    private val ttlMs: Long = DEFAULT_TTL_MS,
    private val database: SightingDatabase = SightingDatabase.get(context),
) {
    private val dao = database.sightingDao()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    private fun <T> blocking(block: suspend () -> T): T =
        runBlocking(scope.coroutineContext) { block() }

    /** first=min / last=max / maxRssi=max / count+1 の read-modify-write を 1 トランザクションで行う。 */
    fun record(token: String, nowMs: Long, rssi: Int) {
        blocking {
            database.withTransaction {
                val old = dao.find(token)
                val merged = if (old == null) {
                    Aggregate(nowMs, nowMs, rssi, 1)
                } else {
                    Aggregate(
                        minOf(old.firstMs, nowMs),
                        maxOf(old.lastMs, nowMs),
                        maxOf(old.maxRssi, rssi),
                        old.count + 1,
                    )
                }
                dao.upsert(merged.toEntity(token))
            }
        }
    }

    /**
     * [nowMs] 時点で生きている entry を返し、全行を消す。
     * `lastMs <= nowMs - ttlMs` の entry は返さずに捨てる (境界の等値は捨てる)。
     */
    fun drain(nowMs: Long): Map<String, Aggregate> = blocking {
        database.withTransaction {
            dao.deleteExpired(nowMs - ttlMs)
            val live = dao.getAll()
            dao.deleteAll()
            live.associate { it.token to it.toAggregate() }
        }
    }

    /** 現在保持している行数。 */
    fun pending(): Int = blocking { dao.count() }

    companion object {
        const val DEFAULT_TTL_MS = 24L * 60 * 60 * 1000
    }
}
