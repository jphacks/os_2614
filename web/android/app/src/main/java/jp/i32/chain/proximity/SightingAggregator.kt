package jp.i32.chain.proximity

import java.util.concurrent.ConcurrentHashMap

/** token ごとの sighting 集約。 */
data class Aggregate(val firstMs: Long, val lastMs: Long, val maxRssi: Int, val count: Int)

/**
 * token ごとに sighting を集約する純粋なストア。Android 非依存。
 * record は first=min / last=max / maxRssi=max / count+1 でマージする。
 * drain は [ttlMs] を超えて古くなった entry (lastMs <= nowMs - ttlMs) を送らずに捨て、
 * 残りを返したうえで全体をクリアする。
 */
class SightingAggregator(private val ttlMs: Long = 24L * 60 * 60 * 1000) {
    private val entries = ConcurrentHashMap<String, Aggregate>()

    fun record(token: String, nowMs: Long, rssi: Int) {
        entries.compute(token) { _, old ->
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
        }
    }

    /**
     * [nowMs] 時点で生きている entry を返し、map 全体をクリアする。
     * lastMs <= nowMs - ttlMs の entry は返さずに捨てる。
     */
    fun drain(nowMs: Long): Map<String, Aggregate> {
        val cutoff = nowMs - ttlMs
        val result = HashMap<String, Aggregate>()
        for (token in entries.keys.toList()) {
            val agg = entries.remove(token) ?: continue
            if (agg.lastMs > cutoff) result[token] = agg
        }
        return result
    }

    /**
     * [nowMs] 時点で生きた entry 数。TTL 切れ (`lastMs <= nowMs - ttlMs`) は数えない (F-04)。
     * [drain] が返す集合と同じ規則。
     */
    fun pending(nowMs: Long): Int {
        val cutoff = nowMs - ttlMs
        return entries.values.count { it.lastMs > cutoff }
    }
}
