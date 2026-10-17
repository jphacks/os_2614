package jp.i32.chain.haptics

import android.view.HapticFeedbackConstants
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin

/**
 * システムの触覚フィードバック。契約は web/src/device/haptics/index.ts。
 * @capacitor/haptics は VibrationEffect の波形を鳴らすため、端末ごとの「タップ感」にならない。
 * ここでは WebView の performHapticFeedback で CONTEXT_CLICK を鳴らし、端末の設定 (タッチ操作のバイブ) に従う。
 */
@CapacitorPlugin(name = "ChainHaptics")
class ChainHapticsPlugin : Plugin() {
    @PluginMethod
    fun contextClick(call: PluginCall) {
        // 触覚は演出なので、鳴らせなくても成功として返す
        val view = bridge?.webView
        if (view == null) {
            call.resolve()
            return
        }
        activity.runOnUiThread {
            view.performHapticFeedback(HapticFeedbackConstants.CONTEXT_CLICK)
            call.resolve()
        }
    }
}
