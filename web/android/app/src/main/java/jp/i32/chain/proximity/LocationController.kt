package jp.i32.chain.proximity

import android.annotation.SuppressLint
import android.content.Context
import android.location.Location
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import com.getcapacitor.JSObject
import com.google.android.gms.location.LocationCallback
import com.google.android.gms.location.LocationRequest
import com.google.android.gms.location.LocationResult
import com.google.android.gms.location.LocationServices
import com.google.android.gms.location.Priority

/**
 * 前面での位置取得 (specification/android/README.md §9)。権限の確認は呼び出し側で行う。
 * 精度やモックで弾くのは backend とその手前の JS の役目で、ここでは isMock を付けて返すだけにする。
 */
class LocationController(context: Context) {
    private val client = LocationServices.getFusedLocationProviderClient(context)
    private val handler = Handler(Looper.getMainLooper())
    private var callback: LocationCallback? = null
    private var intervalMs: Long? = null
    private var paused = false

    /**
     * 1 回だけ取得する。最初の fix は粗いことが多い (Wi-Fi / 基地局由来や GNSS の収束前) ので、
     * timeoutMs の間は更新を受け続け、精度が desiredAccuracyM 以下になった時点で返す。
     * 届かなければ、それまでで最も精度の良い fix を返す。
     */
    @SuppressLint("MissingPermission")
    fun current(
        timeoutMs: Long,
        maxAgeMs: Long,
        desiredAccuracyM: Float,
        onFix: (Location) -> Unit,
        onError: (code: String, message: String) -> Unit,
    ) {
        var best: Location? = null
        var done = false
        lateinit var updates: LocationCallback

        val finish = Runnable {
            if (done) return@Runnable
            done = true
            client.removeLocationUpdates(updates)
            best?.let(onFix) ?: onError("timeout", "位置を取得できませんでした")
        }
        val offer = { location: Location ->
            if (!done) {
                best = better(best, location)
                if (isAccurate(best, desiredAccuracyM)) {
                    handler.removeCallbacks(finish)
                    finish.run()
                }
            }
        }
        updates = object : LocationCallback() {
            override fun onLocationResult(result: LocationResult) {
                result.locations.forEach(offer)
            }
        }

        if (maxAgeMs > 0) {
            client.lastLocation.addOnSuccessListener { location ->
                if (location != null && ageMs(location) <= maxAgeMs) offer(location)
            }
        }
        val request = LocationRequest.Builder(Priority.PRIORITY_HIGH_ACCURACY, CURRENT_INTERVAL_MS)
            .setMinUpdateIntervalMillis(CURRENT_INTERVAL_MS / 2)
            .setMaxUpdateAgeMillis(maxAgeMs)
            .setDurationMillis(timeoutMs)
            .build()
        client.requestLocationUpdates(request, updates, Looper.getMainLooper())
            .addOnFailureListener {
                if (done) return@addOnFailureListener
                done = true
                handler.removeCallbacks(finish)
                onError("unavailable", it.message ?: "位置を取得できませんでした")
            }
        handler.postDelayed(finish, timeoutMs)
    }

    fun startUpdates(intervalMs: Long, onFix: (Location) -> Unit) {
        stopUpdates()
        this.intervalMs = intervalMs
        callback = object : LocationCallback() {
            override fun onLocationResult(result: LocationResult) {
                result.lastLocation?.let(onFix)
            }
        }
        if (!paused) request()
    }

    fun stopUpdates() {
        callback?.let { client.removeLocationUpdates(it) }
        callback = null
        intervalMs = null
    }

    /** アプリが背面に回ったら止め、前面に戻ったら再開する (背面での位置送信は後回し: backend README L2) */
    fun pause() {
        paused = true
        callback?.let { client.removeLocationUpdates(it) }
    }

    fun resume() {
        paused = false
        if (callback != null) request()
    }

    @SuppressLint("MissingPermission")
    private fun request() {
        val interval = intervalMs ?: return
        val callback = callback ?: return
        val request = LocationRequest.Builder(Priority.PRIORITY_HIGH_ACCURACY, interval)
            .setMinUpdateIntervalMillis(interval / 2)
            .build()
        client.requestLocationUpdates(request, callback, Looper.getMainLooper())
    }

    companion object {
        /** 精度が無い位置は、backend の上限 (200 m) を確実に超える値にして弾かれるようにする */
        private const val UNKNOWN_ACCURACY_M = 1_000_000.0

        /** current() で更新を受ける間隔。GNSS は 1 Hz なのでこれより短くしても届かない */
        private const val CURRENT_INTERVAL_MS = 1_000L

        private fun ageMs(location: Location): Long =
            (SystemClock.elapsedRealtimeNanos() - location.elapsedRealtimeNanos) / 1_000_000

        private fun accuracyOf(location: Location): Float =
            if (location.hasAccuracy()) location.accuracy else Float.MAX_VALUE

        private fun isAccurate(location: Location?, desiredAccuracyM: Float): Boolean =
            location != null && location.hasAccuracy() && location.accuracy <= desiredAccuracyM

        /** 精度の良い方。同じなら新しい方 */
        internal fun better(current: Location?, next: Location): Location {
            if (current == null) return next
            val a = accuracyOf(current)
            val b = accuracyOf(next)
            return when {
                b < a -> next
                b > a -> current
                next.elapsedRealtimeNanos >= current.elapsedRealtimeNanos -> next
                else -> current
            }
        }

        fun toJson(location: Location): JSObject {
            val isMock = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) location.isMock else {
                @Suppress("DEPRECATION")
                location.isFromMockProvider
            }
            return JSObject()
                .put("lat", location.latitude)
                .put("lon", location.longitude)
                .put("accuracyM", if (location.hasAccuracy()) location.accuracy.toDouble() else UNKNOWN_ACCURACY_M)
                .put("recordedAt", ProximityState.iso(location.time + ProximityState.clockOffsetMs))
                .put("isMock", isMock)
        }
    }
}
