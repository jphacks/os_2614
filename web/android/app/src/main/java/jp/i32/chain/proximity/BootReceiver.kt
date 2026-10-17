package jp.i32.chain.proximity

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log

/**
 * 端末再起動後、ユーザーが start していた場合だけすれ違いモード (FGS) を再開する。
 * ACTION_LOCKED_BOOT_COMPLETED は扱わない (フラグは credential-encrypted storage にあり、
 * 直接ブートではまだ読めないため)。
 */
class BootReceiver : BroadcastReceiver() {
    companion object {
        private const val TAG = "ChainProximity"
    }

    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return

        ProximityState.load(context)
        if (!BootState.isStartedByUser(context)) return

        try {
            ProximityService.start(context)
        } catch (e: Exception) {
            // Android 12+ では背景からの FGS 起動が ForegroundServiceStartNotAllowedException で弾かれ得る
            Log.w(TAG, "boot: failed to restart proximity service", e)
        }
    }
}
