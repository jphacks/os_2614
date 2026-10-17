package jp.i32.chain.proximity

/**
 * 時計補正値 (offsetMs) の平滑化。直近 [maxSamples] 件の中央値を返す。
 * 偶数件のときは下半分の中央 (sorted[size / 2]) を使う。Android 非依存。
 */
class ClockOffsetEstimator(private val maxSamples: Int = 5) {
    private val samples = ArrayDeque<Long>()

    /** サンプルを追加し、直近 [maxSamples] 件の中央値を返す。 */
    @Synchronized
    fun add(offsetMs: Long): Long {
        samples.addLast(offsetMs)
        while (samples.size > maxSamples) samples.removeFirst()
        return median()
    }

    /** 現在のサンプルの中央値。サンプルが無ければ 0L。 */
    @Synchronized
    fun current(): Long = median()

    @Synchronized
    fun clear() {
        samples.clear()
    }

    private fun median(): Long {
        if (samples.isEmpty()) return 0L
        val sorted = samples.sorted()
        return sorted[sorted.size / 2]
    }
}
