package jp.i32.chain.proximity

import android.content.Context
import android.util.Base64
import com.getcapacitor.JSArray
import com.getcapacitor.JSObject
import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant
import java.util.concurrent.ConcurrentHashMap

/**
 * プラグイン (WebView 側) と ProximityService (FGS 側) が共有する状態。
 * どちらのライフサイクルにも属さないので process 単位の object に置く。
 * token と時刻補正は SharedPreferences に残し、START_STICKY での再生成後も発信を続けられるようにする。
 */
object ProximityState {
    private const val PREFS = "chain_proximity"
    private const val KEY_TOKENS = "tokens"
    private const val KEY_WINDOW_SEC = "window_sec"
    private const val KEY_CLOCK_OFFSET = "clock_offset_ms"

    /** 同じ token の sighting イベントを JS に送る最短間隔 */
    private const val SIGHTING_EVENT_INTERVAL_MS = 5_000L

    class Token(val windowStartSec: Long, val bytes: ByteArray)

    private data class Aggregate(val firstMs: Long, val lastMs: Long, val maxRssi: Int, val count: Int)

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
    @Volatile var lastError: String? = null

    private val sightings = ConcurrentHashMap<String, Aggregate>()
    private val lastSightingEvent = ConcurrentHashMap<String, Long>()
    private var loaded = false

    /** プラグインが登録する。WebView が無いときは null */
    @Volatile var eventSink: ((String, JSObject) -> Unit)? = null

    /** BleEngine が登録する。token か時刻補正が変わったら呼ばれる */
    @Volatile var onBeaconConfigChanged: (() -> Unit)? = null

    /** backend の時計での現在時刻 */
    fun now(): Long = System.currentTimeMillis() + clockOffsetMs

    @Synchronized
    fun load(context: Context) {
        if (loaded) return
        loaded = true
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        windowSec = prefs.getLong(KEY_WINDOW_SEC, 900)
        clockOffsetMs = prefs.getLong(KEY_CLOCK_OFFSET, 0)
        tokens = runCatching {
            val array = JSONArray(prefs.getString(KEY_TOKENS, "[]"))
            (0 until array.length()).map {
                val item = array.getJSONObject(it)
                Token(item.getLong("w"), decodeToken(item.getString("t")))
            }
        }.getOrDefault(emptyList())
    }

    fun setTokens(context: Context, windowSec: Long, tokens: List<Token>) {
        this.windowSec = windowSec
        this.tokens = tokens.sortedBy { it.windowStartSec }
        val array = JSONArray()
        this.tokens.forEach { array.put(JSONObject().put("w", it.windowStartSec).put("t", encodeToken(it.bytes))) }
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putLong(KEY_WINDOW_SEC, windowSec)
            .putString(KEY_TOKENS, array.toString())
            .apply()
        onBeaconConfigChanged?.invoke()
        emitStatus()
    }

    fun setClockOffset(context: Context, offsetMs: Long) {
        clockOffsetMs = offsetMs
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putLong(KEY_CLOCK_OFFSET, offsetMs)
            .apply()
        onBeaconConfigChanged?.invoke()
        emitStatus()
    }

    /** nowMs (backend の時計) を含む窓の token。古い token は再利用しない */
    fun tokenAt(nowMs: Long): Token? {
        val windowStart = Math.floorDiv(nowMs / 1000, windowSec) * windowSec
        return tokens.firstOrNull { it.windowStartSec == windowStart }
    }

    fun recordSighting(tokenBytes: ByteArray, rssi: Int) {
        val token = encodeToken(tokenBytes)
        val now = now()
        sightings.compute(token) { _, old ->
            if (old == null) Aggregate(now, now, rssi, 1)
            else Aggregate(minOf(old.firstMs, now), maxOf(old.lastMs, now), maxOf(old.maxRssi, rssi), old.count + 1)
        }
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
        for (token in sightings.keys.toList()) {
            val agg = sightings.remove(token) ?: continue
            items.put(
                JSObject()
                    .put("token", token)
                    .put("firstSeenAt", iso(agg.firstMs))
                    .put("lastSeenAt", iso(agg.lastMs))
                    .put("maxRssi", agg.maxRssi)
                    .put("count", agg.count),
            )
        }
        val cutoff = now() - SIGHTING_EVENT_INTERVAL_MS
        lastSightingEvent.entries.removeIf { it.value < cutoff }
        return items
    }

    fun statusJson(): JSObject {
        val validUntil = tokens.lastOrNull()?.let { (it.windowStartSec + windowSec) * 1000 }
        return JSObject()
            .put("running", running)
            .put("bluetoothEnabled", bluetoothEnabled)
            .put("advertising", advertising)
            .put("scanning", scanning)
            .put("currentWindowStart", currentWindowStartSec?.let { iso(it * 1000) } ?: JSONObject.NULL)
            .put("tokensValidUntil", validUntil?.let { iso(it) } ?: JSONObject.NULL)
            .put("pendingSightings", sightings.size)
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
}
