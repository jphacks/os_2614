package jp.i32.chain.proximity

import android.annotation.SuppressLint
import android.content.Context
import android.location.Location
import android.os.Build
import android.os.Looper
import com.getcapacitor.JSObject
import com.google.android.gms.location.CurrentLocationRequest
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
    private var callback: LocationCallback? = null
    private var intervalMs: Long? = null
    private var paused = false

    @SuppressLint("MissingPermission")
    fun current(
        timeoutMs: Long,
        maxAgeMs: Long,
        onFix: (Location) -> Unit,
        onError: (code: String, message: String) -> Unit,
    ) {
        val request = CurrentLocationRequest.Builder()
            .setPriority(Priority.PRIORITY_HIGH_ACCURACY)
            .setDurationMillis(timeoutMs)
            .setMaxUpdateAgeMillis(maxAgeMs)
            .build()
        client.getCurrentLocation(request, null)
            .addOnSuccessListener { location ->
                if (location == null) onError("timeout", "位置を取得できませんでした") else onFix(location)
            }
            .addOnFailureListener { onError("unavailable", it.message ?: "位置を取得できませんでした") }
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
