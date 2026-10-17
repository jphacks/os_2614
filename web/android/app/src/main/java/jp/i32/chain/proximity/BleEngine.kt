package jp.i32.chain.proximity

import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothManager
import android.bluetooth.le.AdvertiseData
import android.bluetooth.le.AdvertisingSet
import android.bluetooth.le.AdvertisingSetCallback
import android.bluetooth.le.AdvertisingSetParameters
import android.bluetooth.le.ScanCallback
import android.bluetooth.le.ScanFilter
import android.bluetooth.le.ScanResult
import android.bluetooth.le.ScanSettings
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import android.util.Log
import androidx.core.content.ContextCompat

/**
 * advertise と scan (specification/android/README.md §6, §7)。
 * すべての操作をメインスレッドの handler 上で行う。
 */
class BleEngine(
    private val context: Context,
    /** 権限の取り消しなど、続けられない状態になったときに呼ぶ */
    private val onFatal: () -> Unit,
) {
    companion object {
        private const val TAG = "ChainProximity"

        const val COMPANY_ID = 0xFFFF
        private val PREFIX = byteArrayOf(0x43, 0x48, 0x01) // "CH" + version
        private const val TOKEN_LENGTH = 16

        /** 30 分を超える scan は OS に opportunistic へ格下げされるので、その前に張り直す */
        private const val SCAN_RESTART_MS = 25 * 60 * 1000L
        /** startScan は 30 秒に 5 回まで (超えると OS が黙って結果を止める) */
        private const val SCAN_START_WINDOW_MS = 30_000L
        private const val SCAN_START_LIMIT = 5
        private const val SCAN_RETRY_MS = 30_000L
    }

    private val handler = Handler(Looper.getMainLooper())
    private val adapter: BluetoothAdapter? = context.getSystemService(BluetoothManager::class.java)?.adapter

    private var advertisingCallback: AdvertisingSetCallback? = null
    private var scanCallback: ScanCallback? = null
    private val scanStarts = ArrayDeque<Long>()
    private var screenOn = context.getSystemService(PowerManager::class.java)?.isInteractive ?: true
    private var started = false

    private val rotate = Runnable { restartAdvertising() }
    private val restartScan = Runnable { restartScanning() }

    private val bluetoothReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            when (intent.getIntExtra(BluetoothAdapter.EXTRA_STATE, BluetoothAdapter.ERROR)) {
                BluetoothAdapter.STATE_OFF -> handler.post { onBluetoothOff() }
                BluetoothAdapter.STATE_ON -> handler.post { onBluetoothOn() }
            }
        }
    }

    private val screenReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            val on = intent.action == Intent.ACTION_SCREEN_ON
            if (on == screenOn) return
            screenOn = on
            handler.post { restartScanning() }
        }
    }

    fun start() = handler.post {
        if (started) return@post
        started = true
        ContextCompat.registerReceiver(
            context, bluetoothReceiver,
            IntentFilter(BluetoothAdapter.ACTION_STATE_CHANGED), ContextCompat.RECEIVER_NOT_EXPORTED,
        )
        ContextCompat.registerReceiver(
            context, screenReceiver,
            IntentFilter().apply {
                addAction(Intent.ACTION_SCREEN_ON)
                addAction(Intent.ACTION_SCREEN_OFF)
            },
            ContextCompat.RECEIVER_NOT_EXPORTED,
        )
        ProximityState.onBeaconConfigChanged = { handler.post { restartAdvertising() } }

        if (adapter == null) {
            setError("bluetooth_unsupported")
            return@post
        }
        if (adapter.isEnabled) onBluetoothOn() else onBluetoothOff()
    }

    fun stop() = handler.post {
        if (!started) return@post
        started = false
        ProximityState.onBeaconConfigChanged = null
        runCatching { context.unregisterReceiver(bluetoothReceiver) }
        runCatching { context.unregisterReceiver(screenReceiver) }
        stopAdvertising()
        stopScanning()
        handler.removeCallbacksAndMessages(null)
        ProximityState.emitStatus()
    }

    private fun onBluetoothOn() {
        ProximityState.bluetoothEnabled = true
        if (ProximityState.lastError == "bluetooth_off") ProximityState.lastError = null
        restartAdvertising()
        restartScanning()
    }

    private fun onBluetoothOff() {
        ProximityState.bluetoothEnabled = false
        stopAdvertising()
        stopScanning()
        setError("bluetooth_off")
    }

    // ---- advertise ----

    private fun restartAdvertising() {
        if (!started) return
        stopAdvertising()
        startAdvertising()
    }

    private fun startAdvertising() {
        if (adapter?.isEnabled != true) return
        val advertiser = adapter.bluetoothLeAdvertiser ?: return setError("advertise_failed")
        val now = ProximityState.now()
        val token = ProximityState.tokenAt(now)
        if (token == null) {
            // 古い token は再利用しない。scan だけ続ける
            ProximityState.currentWindowStartSec = null
            setError("tokens_exhausted")
            return
        }

        val parameters = AdvertisingSetParameters.Builder()
            .setLegacyMode(true)
            .setConnectable(false)
            .setScannable(false)
            .setInterval(AdvertisingSetParameters.INTERVAL_MEDIUM)
            .setTxPowerLevel(AdvertisingSetParameters.TX_POWER_MEDIUM)
            .build()
        val data = AdvertiseData.Builder()
            .setIncludeDeviceName(false)
            .setIncludeTxPowerLevel(false)
            .addManufacturerData(COMPANY_ID, PREFIX + token.bytes)
            .build()
        val callback = object : AdvertisingSetCallback() {
            override fun onAdvertisingSetStarted(set: AdvertisingSet?, txPower: Int, status: Int) {
                if (advertisingCallback !== this) return
                if (status == ADVERTISE_SUCCESS) {
                    ProximityState.advertising = true
                    ProximityState.currentWindowStartSec = token.windowStartSec
                    clearError("advertise_failed", "tokens_exhausted")
                } else {
                    Log.w(TAG, "advertise failed: $status")
                    ProximityState.advertising = false
                    setError("advertise_failed")
                }
                ProximityState.emitStatus()
            }
        }

        try {
            advertisingCallback = callback
            advertiser.startAdvertisingSet(parameters, data, null, null, null, callback)
        } catch (e: SecurityException) {
            advertisingCallback = null
            return permissionRevoked(e)
        }

        // 窓の境界 (backend の時計) で作り直す。作り直すと送信元アドレスも変わることを期待している (README V2)
        val windowEndMs = (token.windowStartSec + ProximityState.windowSec) * 1000
        handler.removeCallbacks(rotate)
        handler.postDelayed(rotate, (windowEndMs - now).coerceAtLeast(1_000))
    }

    private fun stopAdvertising() {
        handler.removeCallbacks(rotate)
        val callback = advertisingCallback ?: return
        advertisingCallback = null
        try {
            adapter?.bluetoothLeAdvertiser?.stopAdvertisingSet(callback)
        } catch (e: SecurityException) {
            Log.w(TAG, "stopAdvertisingSet", e)
        } catch (e: IllegalStateException) {
            // Bluetooth がオフになった直後
        }
        ProximityState.advertising = false
        ProximityState.currentWindowStartSec = null
        ProximityState.emitStatus()
    }

    // ---- scan ----

    private fun restartScanning() {
        if (!started) return
        stopScanning()
        startScanning()
    }

    private fun startScanning() {
        if (adapter?.isEnabled != true) return
        val scanner = adapter.bluetoothLeScanner ?: return setError("scan_failed")

        val now = System.currentTimeMillis()
        while (scanStarts.isNotEmpty() && now - scanStarts.first() > SCAN_START_WINDOW_MS) scanStarts.removeFirst()
        if (scanStarts.size >= SCAN_START_LIMIT) {
            handler.removeCallbacks(restartScan)
            handler.postDelayed(restartScan, SCAN_START_WINDOW_MS - (now - scanStarts.first()) + 500)
            return
        }

        val filter = ScanFilter.Builder()
            .setManufacturerData(COMPANY_ID, PREFIX, byteArrayOf(-1, -1, -1))
            .build()
        val settings = ScanSettings.Builder()
            .setScanMode(if (screenOn) ScanSettings.SCAN_MODE_BALANCED else ScanSettings.SCAN_MODE_LOW_POWER)
            .setCallbackType(ScanSettings.CALLBACK_TYPE_ALL_MATCHES)
            .setReportDelay(0)
            .build()
        val callback = object : ScanCallback() {
            override fun onScanResult(callbackType: Int, result: ScanResult) = handle(result)

            override fun onBatchScanResults(results: MutableList<ScanResult>) = results.forEach(::handle)

            override fun onScanFailed(errorCode: Int) {
                Log.w(TAG, "scan failed: $errorCode")
                handler.post {
                    if (scanCallback !== this) return@post
                    scanCallback = null
                    ProximityState.scanning = false
                    setError("scan_failed")
                    handler.removeCallbacks(restartScan)
                    handler.postDelayed(restartScan, SCAN_RETRY_MS)
                }
            }
        }

        try {
            scanner.startScan(listOf(filter), settings, callback)
        } catch (e: SecurityException) {
            return permissionRevoked(e)
        }
        scanStarts.addLast(now)
        scanCallback = callback
        ProximityState.scanning = true
        clearError("scan_failed")
        ProximityState.emitStatus()
        handler.removeCallbacks(restartScan)
        handler.postDelayed(restartScan, SCAN_RESTART_MS)
    }

    private fun stopScanning() {
        handler.removeCallbacks(restartScan)
        val callback = scanCallback ?: return
        scanCallback = null
        try {
            adapter?.bluetoothLeScanner?.stopScan(callback)
        } catch (e: SecurityException) {
            Log.w(TAG, "stopScan", e)
        } catch (e: IllegalStateException) {
            // Bluetooth がオフになった直後
        }
        ProximityState.scanning = false
        ProximityState.emitStatus()
    }

    private fun handle(result: ScanResult) {
        val data = result.scanRecord?.getManufacturerSpecificData(COMPANY_ID) ?: return
        if (data.size < PREFIX.size + TOKEN_LENGTH) return
        for (i in PREFIX.indices) if (data[i] != PREFIX[i]) return
        ProximityState.recordSighting(data.copyOfRange(PREFIX.size, PREFIX.size + TOKEN_LENGTH), result.rssi)
    }

    // ---- errors ----

    private fun setError(error: String) {
        ProximityState.lastError = error
        ProximityState.emitStatus()
    }

    private fun clearError(vararg errors: String) {
        if (ProximityState.lastError in errors) ProximityState.lastError = null
    }

    private fun permissionRevoked(e: SecurityException) {
        Log.w(TAG, "permission revoked", e)
        setError("permission_revoked")
        onFatal()
    }
}
