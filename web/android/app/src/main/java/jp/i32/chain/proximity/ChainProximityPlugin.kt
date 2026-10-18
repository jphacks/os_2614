package jp.i32.chain.proximity

import android.Manifest
import android.bluetooth.BluetoothManager
import android.os.Build
import com.getcapacitor.JSObject
import com.getcapacitor.PermissionState
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import com.getcapacitor.annotation.Permission
import com.getcapacitor.annotation.PermissionCallback
import java.time.Instant

/**
 * WebView との窓口。契約は web/src/device/proximity/definitions.ts。
 * BLE は ProximityService、位置は LocationController に任せ、ここでは引数の検証と権限だけを扱う。
 */
@CapacitorPlugin(
    name = "ChainProximity",
    permissions = [
        Permission(alias = ChainProximityPlugin.BLUETOOTH, strings = [Manifest.permission.BLUETOOTH_SCAN, Manifest.permission.BLUETOOTH_ADVERTISE]),
        Permission(alias = ChainProximityPlugin.LOCATION, strings = [Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION]),
        Permission(alias = ChainProximityPlugin.NOTIFICATIONS, strings = [Manifest.permission.POST_NOTIFICATIONS]),
    ],
)
class ChainProximityPlugin : Plugin() {
    companion object {
        const val BLUETOOTH = "bluetooth"
        const val LOCATION = "location"
        const val NOTIFICATIONS = "notifications"

        private const val TOKEN_LENGTH = 16
        private const val DEFAULT_LOCATION_TIMEOUT_MS = 10_000L
        private const val DEFAULT_LOCATION_MAX_AGE_MS = 30_000L
    }

    private lateinit var location: LocationController

    override fun load() {
        ProximityState.load(context)
        ProximityState.bluetoothEnabled = context.getSystemService(BluetoothManager::class.java)?.adapter?.isEnabled == true
        ProximityState.eventSink = { event, data -> notifyListeners(event, data) }
        location = LocationController(context)
    }

    override fun handleOnDestroy() {
        ProximityState.eventSink = null
        location.stopUpdates()
        super.handleOnDestroy()
    }

    override fun handleOnPause() {
        location.pause()
        super.handleOnPause()
    }

    override fun handleOnResume() {
        super.handleOnResume()
        location.resume()
    }

    // ---- 権限 ----

    @PluginMethod
    override fun checkPermissions(call: PluginCall) {
        call.resolve(permissionStatus())
    }

    @PluginMethod
    override fun requestPermissions(call: PluginCall) {
        val requested = call.getArray("permissions")?.toList<String>() ?: listOf(BLUETOOTH, LOCATION, NOTIFICATIONS)
        val aliases = requested.mapNotNull { permission ->
            when (permission) {
                // Android 11 以下の BLE scan は位置権限で許可される
                BLUETOOTH -> if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) BLUETOOTH else LOCATION
                LOCATION -> LOCATION
                NOTIFICATIONS -> if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) NOTIFICATIONS else null
                else -> return call.reject("unknown permission: $permission")
            }
        }.distinct().filter { getPermissionState(it) != PermissionState.GRANTED }

        if (aliases.isEmpty()) {
            call.resolve(permissionStatus())
        } else {
            requestPermissionForAliases(aliases.toTypedArray(), call, "permissionsCallback")
        }
    }

    @PermissionCallback
    private fun permissionsCallback(call: PluginCall) {
        call.resolve(permissionStatus())
    }

    private fun permissionStatus(): JSObject = JSObject()
        .put(BLUETOOTH, bluetoothState().toString())
        .put(LOCATION, getPermissionState(LOCATION).toString())
        .put(
            NOTIFICATIONS,
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) getPermissionState(NOTIFICATIONS).toString()
            else PermissionState.GRANTED.toString(),
        )

    private fun bluetoothState(): PermissionState =
        getPermissionState(if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) BLUETOOTH else LOCATION)

    // ---- すれ違い ----

    @PluginMethod
    fun setBeaconTokens(call: PluginCall) {
        val windowSec = call.number("windowSec")?.toLong() ?: return call.reject("windowSec is required")
        if (windowSec <= 0) return call.reject("windowSec must be positive")
        val items = call.getArray("tokens") ?: return call.reject("tokens is required")

        val tokens = try {
            (0 until items.length()).map { i ->
                val item = items.getJSONObject(i)
                val bytes = ProximityState.decodeToken(item.getString("token"))
                require(bytes.size == TOKEN_LENGTH) { "token must be $TOKEN_LENGTH bytes" }
                ProximityState.Token(Instant.parse(item.getString("windowStart")).epochSecond, bytes)
            }
        } catch (e: Exception) {
            return call.reject("invalid tokens: ${e.message}")
        }

        ProximityState.setTokens(windowSec, tokens)
        call.resolve()
    }

    @PluginMethod
    fun setClockOffset(call: PluginCall) {
        val offset = call.number("offsetMs")?.toDouble() ?: return call.reject("offsetMs is required")
        ProximityState.setClockOffset(offset.toLong())
        call.resolve()
    }

    @PluginMethod
    fun start(call: PluginCall) {
        if (bluetoothState() != PermissionState.GRANTED) {
            return call.reject("Bluetooth の権限がありません", "denied")
        }
        if (context.getSystemService(BluetoothManager::class.java)?.adapter == null) {
            return call.reject("この端末は Bluetooth に対応していません", "bluetooth_unsupported")
        }
        try {
            ProximityService.start(context)
        } catch (e: Exception) {
            // バックグラウンドからの起動 (ForegroundServiceStartNotAllowedException) など
            return call.reject("すれ違いモードを開始できませんでした", "start_failed", e)
        }
        call.resolve()
    }

    @PluginMethod
    fun stop(call: PluginCall) {
        ProximityService.stop(context)
        call.resolve()
    }

    @PluginMethod
    fun getStatus(call: PluginCall) {
        call.resolve(ProximityState.statusJson())
    }

    @PluginMethod
    fun drainSightings(call: PluginCall) {
        call.resolve(JSObject().put("items", ProximityState.drainSightings()))
    }

    // ---- 位置 ----

    @PluginMethod
    fun getCurrentLocation(call: PluginCall) {
        if (getPermissionState(LOCATION) != PermissionState.GRANTED) {
            return call.reject("位置の権限がありません", "denied")
        }
        location.current(
            timeoutMs = call.number("timeoutMs")?.toLong() ?: DEFAULT_LOCATION_TIMEOUT_MS,
            maxAgeMs = call.number("maxAgeMs")?.toLong() ?: DEFAULT_LOCATION_MAX_AGE_MS,
            onFix = { call.resolve(LocationController.toJson(it)) },
            onError = { code, message -> call.reject(message, code) },
        )
    }

    @PluginMethod
    fun startLocationUpdates(call: PluginCall) {
        if (getPermissionState(LOCATION) != PermissionState.GRANTED) {
            return call.reject("位置の権限がありません", "denied")
        }
        val intervalMs = call.number("intervalMs")?.toLong() ?: return call.reject("intervalMs is required")
        if (intervalMs < 1_000) return call.reject("intervalMs must be at least 1000")
        location.startUpdates(intervalMs) { notifyListeners("location", LocationController.toJson(it)) }
        call.resolve()
    }

    @PluginMethod
    fun stopLocationUpdates(call: PluginCall) {
        location.stopUpdates()
        call.resolve()
    }
}

/**
 * JS の数値。JSON 由来の整数は Integer で届くが、PluginCall.getLong / getDouble は
 * Long / Double しか受けず、900 のような値を null にしてしまう。Number なら何でも受ける。
 */
private fun PluginCall.number(name: String): Number? = data.opt(name) as? Number
