package jp.i32.chain.proximity

/**
 * 窓 (window) の選択に関する純粋な計算。
 * ProximityState.tokenAt と同一の算術を保持し、Android に依存しない。
 */
object BeaconWindow {
    /**
     * nowMs (backend の時計, ミリ秒) を含む窓の開始秒。
     * ProximityState:97 の `Math.floorDiv(nowMs / 1000, windowSec) * windowSec` と完全に同一。
     * 負の nowMs でも同じ結果になるよう Math.floorDiv を使う。
     */
    fun windowStartSec(nowMs: Long, windowSec: Long): Long =
        Math.floorDiv(nowMs / 1000, windowSec) * windowSec

    /**
     * nowMs を含む窓の token。窓が一致する最初の token を返し、無ければ null。
     * 古い token は再利用しない。
     */
    fun tokenFor(tokens: List<Pair<Long, ByteArray>>, nowMs: Long, windowSec: Long): Pair<Long, ByteArray>? {
        val windowStart = windowStartSec(nowMs, windowSec)
        return tokens.firstOrNull { it.first == windowStart }
    }
}
