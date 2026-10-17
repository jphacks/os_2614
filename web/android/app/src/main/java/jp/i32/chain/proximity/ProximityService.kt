package jp.i32.chain.proximity

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat

/**
 * すれ違いモードの Foreground Service (type connectedDevice)。
 * BLE を WebView から切り離してこの中で動かす (docs/research/04-capacitor.md §3.3)。
 */
class ProximityService : Service() {
    companion object {
        private const val TAG = "ChainProximity"
        private const val ACTION_STOP = "jp.i32.chain.proximity.STOP"
        private const val CHANNEL_ID = "proximity"
        private const val NOTIFICATION_ID = 2614

        fun start(context: Context) {
            // startedByUser=true: 再起動後 (BootReceiver) にこの FGS を復帰させる
            BootState.setStartedByUser(context, true)
            ContextCompat.startForegroundService(context, Intent(context, ProximityService::class.java))
        }

        fun stop(context: Context) {
            // startedByUser=false: 再起動後の復帰対象から外す (START_NOT_STICKY で止める)
            BootState.setStartedByUser(context, false)
            context.stopService(Intent(context, ProximityService::class.java))
        }
    }

    private var engine: BleEngine? = null

    override fun onCreate() {
        super.onCreate()
        ProximityState.load(this)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            // startedByUser=false: 通知の stop でも再起動復帰を断つ
            BootState.setStartedByUser(this, false)
            stopSelf()
            return START_NOT_STICKY
        }

        try {
            ServiceCompat.startForeground(
                this,
                NOTIFICATION_ID,
                buildNotification(),
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE else 0,
            )
        } catch (e: Exception) {
            // 権限が無い、またはバックグラウンドからの起動が許されない (Android 12+)
            Log.w(TAG, "startForeground failed", e)
            ProximityState.lastError = "permission_revoked"
            // startedByUser=false: 二度と起動できないサービスを再起動後 (BootReceiver) に再試行させない
            BootState.setStartedByUser(this, false)
            stopSelf()
            return START_NOT_STICKY
        }

        if (engine == null) {
            engine = BleEngine(this) {
                // F-13: a transient SecurityException (e.g. a race while Bluetooth toggles) is not a
                // revocation. Only discard the persisted "on" intent when the runtime permission is
                // genuinely gone (specification/android/README.md §5). If it is still granted, the
                // failure is transient, so keep the intent and let the boot restore retry.
                if (RevocationPolicy.shouldClearPersistedIntent(blePermissionGranted())) {
                    // startedByUser=false: 権限取消でユーザーの「オン」意思を履行できないため boot 再試行を止める
                    BootState.setStartedByUser(this, false)
                } else {
                    Log.w(TAG, "permission still granted; treating revocation signal as transient")
                }
                stopSelf()
            }.also { it.start() }
        }
        ProximityState.running = true
        ProximityState.emitStatus()
        // プロセスが落とされても OS に再生成させる。token は ProximityState が復元する
        return START_STICKY
    }

    override fun onDestroy() {
        engine?.stop()
        engine = null
        ProximityState.running = false
        ProximityState.advertising = false
        ProximityState.scanning = false
        ProximityState.emitStatus()
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    /**
     * Runtime permission state for the BLE engine (Android 12+/S gates BLE with
     * BLUETOOTH_SCAN/ADVERTISE; older releases use location for scan). The behaviour contract
     * itself is pure and covered by [RevocationPolicy].
     */
    private fun blePermissionGranted(): Boolean = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
        ContextCompat.checkSelfPermission(this, Manifest.permission.BLUETOOTH_SCAN) ==
            PackageManager.PERMISSION_GRANTED &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.BLUETOOTH_ADVERTISE) ==
            PackageManager.PERMISSION_GRANTED
    } else {
        ContextCompat.checkSelfPermission(this, Manifest.permission.ACCESS_FINE_LOCATION) ==
            PackageManager.PERMISSION_GRANTED
    }

    private fun buildNotification(): Notification {
        val manager = getSystemService(NotificationManager::class.java)
        if (manager.getNotificationChannel(CHANNEL_ID) == null) {
            manager.createNotificationChannel(
                NotificationChannel(CHANNEL_ID, "すれ違いモード", NotificationManager.IMPORTANCE_LOW),
            )
        }

        val open = packageManager.getLaunchIntentForPackage(packageName)?.let {
            PendingIntent.getActivity(this, 0, it, PendingIntent.FLAG_IMMUTABLE)
        }
        val stop = PendingIntent.getService(
            this, 1,
            Intent(this, ProximityService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE,
        )

        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_sys_data_bluetooth)
            .setContentTitle("すれ違いモード: オン")
            .setContentIntent(open)
            .addAction(0, "停止", stop)
            .setOngoing(true)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .build()
    }
}
