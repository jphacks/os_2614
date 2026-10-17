package jp.i32.chain.proximity

import android.content.Context
import android.util.Base64
import android.util.Log
import com.getcapacitor.JSArray
import com.getcapacitor.JSObject
import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant
import java.util.concurrent.ConcurrentHashMap

/**
 * statusJson() の lastError に出してよい語彙。
 * spec §2 の 5 語 (permission_revoked / bluetooth_off / advertise_failed / scan_failed / tokens_exhausted)
 * に加えて、FE 契約 `web/src/device/proximity/definitions.ts` の `ProximityError` が持つ
 * `bluetooth_unsupported` を許容する (spec §2 の一覧には無いが凍結済みの契約なので残す)。
 * ここに無い文字列は [ProximityState.lastError] の setter で無視され、statusJson() には出ない。
 * `session_expired` は Phase B の語彙なので意図的に含めない。
 */
internal val PROXIMITY_ERRORS: Set<String> = setOf(
    "permission_revoked",
    "bluetooth_off",
    "advertise_failed",
    "scan_failed",
    "tokens_exhausted",
    "bluetooth_unsupported",
)

/** [value] を lastError として受理してよいか。null は常に可。 */
internal fun isValidProximityError(value: String?): Boolean = value == null || value in PROXIMITY_ERRORS

/**
 * プラグイン (WebView 側) と ProximityService (FGS 側) が共有する状態。
 * どちらのライフサイクルにも属さないので process 単位の object に置く。
 * token と時刻補正は [SecureStore] (Keystore 暗号化) に残し、START_STICKY での再生成後も発信を続けられるようにする。
 * sighting は [SightingStore] (Room) に永続化し、load 前だけ in-memory の [SightingAggregator] を使う。
 */
object ProximityState {
    private const val TAG = "ChainProximity"
    private const val KEY_TOKENS = "tokens"
    private const val KEY_WINDOW_SEC = "window_sec"
    private const val KEY_CLOCK_OFFSET = "clock_offset_ms"

    /** 同じ token の sighting イベントを JS に送る最短間隔 */
    private const val SIGHTING_EVENT_INTERVAL_MS = 5_000L

    /** F-04: throttle マップの上限。超えたら古い順に落とす (adversarial token 増殖の歯止め)。 */
    private const val MAX_SIGHTING_EVENT_ENTRIES = 2_048

    class Token(val windowStartSec: Long, val bytes: ByteArray)

    @Volatile var windowSec: Long = 900
        private set
    @Volatile var tokens: List<Token> = emptyList()
        private set
    @Volatile var clockOffsetMs: Long = 0
        private set

    @Volatile var running = false
    @Volatile var bluetoothEnabled = false
    @Volatile var advertising = false
    @Volatile var scanning = false
    @Volatile var currentWindowStartSec: Long? = null

    /** 語彙外の値は無視する。lastError を statusJson() に出す唯一の経路をここに集約する。 */
    @Volatile var lastError: String? = null
        set(value) {
            if (isValidProximityError(value)) {
                field = value
            } else {
                // F-08: typo / rename で status が黙って null になるのを避け、バグを可視化する。
                runCatching { Log.w(TAG, "dropping unknown lastError: $value") }
                field = null
            }
        }

    private val sightings = SightingAggregator()
    private val sampler = ClockSampler()
    private val lastSightingEvent = ConcurrentHashMap<String, Long>()

    /** Room 版の永続ストア。load に失敗したら null のままにして in-memory 集約にフォールバックする。 */
    @Volatile private var store: SightingStore? = null

    /** Keystore 暗号化ストア。生成に失敗しても平文には決して書かない (null は「永続化しない」の意)。 */
    @Volatile private var secure: SecureStore? = null

    private var loaded = false

    /** プラグインが登録する。WebView が無いときは null */
    @Volatile var eventSink: ((String, JSObject) -> Unit)? = null

    /** BleEngine が登録する。token か時刻補正が変わったら呼ばれる */
    @Volatile var onBeaconConfigChanged: (() -> Unit)? = null

    /** backend の時計での現在時刻 */
    fun now(): Long = System.currentTimeMillis() + clockOffsetMs

    /**
     * load は絶対に例外を投げない。失敗した部品はログだけ残して in-memory 側にフォールバックする。
     *
     * [secureFactory] / [storeFactory] はテスト seam。既定は本番の実装 ([SecureStore.create] /
     * [SightingStore])。`loaded` は初期化が最後まで通ってから立てるので、暗号化値の復号失敗
     * (F-01) などで途中終了しても再ロードできる。
     */
    @Synchronized
    fun load(
        context: Context,
        secureFactory: (Context) -> SecureStore = SecureStore::create,
        storeFactory: (Context) -> SightingStore? = { SightingStore(it) },
    ) {
        if (loaded) return

        val secured = runCatching { secureFactory(context) }
            .getOrElse { e ->
                logWarn("secure store unavailable: ${e.javaClass.simpleName}", e)
                null
            }
        secure = secured
        if (secured != null) {
            if (!secured.usable) logWarn("secure store unusable: ${secured.lastError}")
            // 平文 chain_proximity からの移行は 1 回だけ。冪等なので loaded ガード内で呼ぶ。
            runCatching { secured.migrate(context) }
                .onFailure { logWarn("secure store migration failed", it) }
        }

        // F-01: EncryptedSharedPreferences は READ 時にも SecurityException を投げうる。
        // どの read も guarded で包み、失敗時は既定値に落として load を絶対に throw させない。
        windowSec = guarded("window_sec", 900L) { secured?.getLong(KEY_WINDOW_SEC, 900L) ?: 900L }
        sampler.clear()
        clockOffsetMs = sampler.add(guarded("clock_offset_ms", 0L) { secured?.getLong(KEY_CLOCK_OFFSET, 0L) ?: 0L })
        val tokensJson = guarded("tokens", "[]") { secured?.getString(KEY_TOKENS) ?: "[]" }
        tokens = runCatching {
            val array = JSONArray(tokensJson)
            (0 until array.length()).map {
                val item = array.getJSONObject(it)
                Token(item.getLong("w"), decodeToken(item.getString("t")))
            }
        }.getOrDefault(emptyList())

        store = runCatching { storeFactory(context) }
            .onFailure { logWarn("room sighting store unavailable; using in-memory aggregator", it) }
            .getOrNull()

        loaded = true
    }

    // F-09: `context` は plaintext SharedPreferences 時代の名残で未使用。呼び出し元
    // (ChainProximityPlugin.kt:127) が `setTokens(context, ...)` のままなので、プラグイン側の
    // 更新までの間はシグネチャ互換のため残す (削除はプラグインと同一コミットで行う)。
    fun setTokens(context: Context, windowSec: Long, tokens: List<Token>) {
        this.windowSec = windowSec
        this.tokens = tokens.sortedBy { it.windowStartSec }
        val array = JSONArray()
        this.tokens.forEach { array.put(JSONObject().put("w", it.windowStartSec).put("t", encodeToken(it.bytes))) }
        // 機密値は暗号化ストアのみに書く。平文には決して書かない。
        secure?.let {
            it.putLong(KEY_WINDOW_SEC, windowSec)
            it.putString(KEY_TOKENS, array.toString())
        }
        onBeaconConfigChanged?.invoke()
        emitStatus()
    }

    // F-09: 上と同様、`context` は未使用だが呼び出し互換のため残す。
    fun setClockOffset(context: Context, offsetMs: Long) {
        // 生のサンプルではなく、直近サンプルの中央値を採用して保存する。
        val median = sampler.add(offsetMs)
        clockOffsetMs = median
        secure?.putLong(KEY_CLOCK_OFFSET, median)
        if (sampler.shouldWarn()) logWarn("clock offset is large: ${median}ms")
        onBeaconConfigChanged?.invoke()
        emitStatus()
    }

    /** nowMs (backend の時計) を含む窓の token。古い token は再利用しない */
    fun tokenAt(nowMs: Long): Token? {
        val found = BeaconWindow.tokenFor(tokens.map { it.windowStartSec to it.bytes }, nowMs, windowSec)
        return found?.let { Token(it.first, it.second) }
    }

    fun recordSighting(tokenBytes: ByteArray, rssi: Int) {
        val token = encodeToken(tokenBytes)
        val now = now()
        val active = store
        if (active != null) active.record(token, now, rssi) else sightings.record(token, now, rssi)
        pruneSightingEvents(now)
        val last = lastSightingEvent[token]
        if (last == null || now - last >= SIGHTING_EVENT_INTERVAL_MS) {
            lastSightingEvent[token] = now
            eventSink?.invoke(
                "sighting",
                JSObject().put("token", token).put("rssi", rssi).put("seenAt", iso(now)),
            )
        }
    }

    fun drainSightings(): JSArray {
        val items = JSArray()
        val now = now()
        val active = store
        val drained = if (active != null) active.drain(now) else sightings.drain(now)
        for ((token, agg) in drained) {
            items.put(
                JSObject()
                    .put("token", token)
                    .put("firstSeenAt", iso(agg.firstMs))
                    .put("lastSeenAt", iso(agg.lastMs))
                    .put("maxRssi", agg.maxRssi)
                    .put("count", agg.count),
            )
        }
        pruneSightingEvents(now)
        return items
    }

    fun statusJson(): JSObject {
        val validUntil = tokens.lastOrNull()?.let { (it.windowStartSec + windowSec) * 1000 }
        val now = now()
        return JSObject()
            .put("running", running)
            .put("bluetoothEnabled", bluetoothEnabled)
            .put("advertising", advertising)
            .put("scanning", scanning)
            .put("currentWindowStart", currentWindowStartSec?.let { iso(it * 1000) } ?: JSONObject.NULL)
            .put("tokensValidUntil", validUntil?.let { iso(it) } ?: JSONObject.NULL)
            // F-04: TTL 切れは数えない (drain まで放置しない)。
            .put("pendingSightings", store?.pending(now) ?: sightings.pending(now))
            .put("clockOffsetMs", clockOffsetMs)
            .put("lastError", lastError ?: JSONObject.NULL)
    }

    fun emitStatus() {
        eventSink?.invoke("statusChange", statusJson())
    }

    fun iso(epochMs: Long): String = Instant.ofEpochMilli(epochMs).toString()

    fun encodeToken(bytes: ByteArray): String =
        Base64.encodeToString(bytes, Base64.URL_SAFE or Base64.NO_PADDING or Base64.NO_WRAP)

    fun decodeToken(value: String): ByteArray =
        Base64.decode(value, Base64.URL_SAFE or Base64.NO_PADDING or Base64.NO_WRAP)

    /**
     * F-04: 5 秒 throttle マップを record 時にも剪定し、上限も設ける。
     * (drain まで放置すると、WebView が死んで drain が来ない間 token ごとに増え続ける。)
     */
    private fun pruneSightingEvents(now: Long) {
        val cutoff = now - SIGHTING_EVENT_INTERVAL_MS
        lastSightingEvent.entries.removeIf { it.value < cutoff }
        if (lastSightingEvent.size > MAX_SIGHTING_EVENT_ENTRIES) {
            lastSightingEvent.entries
                .sortedBy { it.value }
                .take(lastSightingEvent.size - MAX_SIGHTING_EVENT_ENTRIES)
                .forEach { lastSightingEvent.remove(it.key) }
        }
    }

    /**
     * 暗号化ストアの read を 1 箇所で握る。EncryptedSharedPreferences は read 時に
     * SecurityException (値の復号失敗) を投げうるため、ここで既定値へ落とす。
     * JVM 単体テストでは android.util.Log が無いので、ログ自体も握る。
     */
    internal fun <T> guarded(what: String, fallback: T, read: () -> T): T =
        runCatching { read() }.getOrElse { e ->
            logWarn("$what read failed: ${e.javaClass.simpleName}", e)
            fallback
        }

    private fun logWarn(message: String, error: Throwable? = null) {
        runCatching {
            if (error != null) Log.w(TAG, message, error) else Log.w(TAG, message)
        }
    }
}
