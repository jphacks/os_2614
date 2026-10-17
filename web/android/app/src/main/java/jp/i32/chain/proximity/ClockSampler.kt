package jp.i32.chain.proximity

import kotlin.math.abs

/**
 * ネイティブ側の時計ずれ (offsetMs) サンプラ。
 *
 * 直近 [samples] 件の中央値を [ClockOffsetEstimator] に委譲して平滑化し、
 * ずれが [logThresholdMs] を超えたら [shouldWarn] で警告対象として知らせる。
 * Android 非依存の純粋なクラス (ログ出力は呼び出し側が行う)。
 */
class ClockSampler(
    samples: Int = 5,
    private val logThresholdMs: Long = 60_000L,
) {
    private val estimator = ClockOffsetEstimator(maxSamples = samples)
    private var currentOffsetMs: Long = 0L

    /** サンプルを追加し、直近 [samples] 件の中央値をキャッシュして返す。 */
    fun add(offsetMs: Long): Long {
        currentOffsetMs = estimator.add(offsetMs)
        return currentOffsetMs
    }

    /** 現在の推定オフセット。サンプルが無ければ 0L。 */
    fun current(): Long = currentOffsetMs

    /** ずれが [logThresholdMs] を超えているか (厳密に大きいときのみ true)。 */
    fun shouldWarn(): Boolean = abs(currentOffsetMs) > logThresholdMs

    /** サンプルと推定値をリセットする。 */
    fun clear() {
        estimator.clear()
        currentOffsetMs = 0L
    }
}
