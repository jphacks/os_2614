package jp.i32.chain.proximity

import android.content.Context

/**
 * 「ユーザーがすれ違いモードを start した」という非機密の boolean を保存する。
 * chain_proximity (token など) とは別のファイルに置き、暗号化ストアも使わない。
 * 端末再起動後の再開判定 (BootReceiver) のためだけに存在する。
 */
object BootState {
    private const val PREFS = "chain_boot"
    private const val KEY_STARTED_BY_USER = "started_by_user"

    fun isStartedByUser(context: Context): Boolean =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .getBoolean(KEY_STARTED_BY_USER, false)

    fun setStartedByUser(context: Context, value: Boolean) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putBoolean(KEY_STARTED_BY_USER, value)
            .apply()
    }
}
