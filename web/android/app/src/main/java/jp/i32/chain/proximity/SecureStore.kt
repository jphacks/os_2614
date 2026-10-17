@file:Suppress("DEPRECATION", "UNCHECKED_CAST")

package jp.i32.chain.proximity

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

/**
 * token や時刻補正のような機密値を Android Keystore の鍵で暗号化して保存する薄いラッパ。
 *
 * [EncryptedSharedPreferences] の生成に失敗した場合 (端末の Keystore 異常など) は
 * **平文への fallback を一切しない**。例外を握って [usable] = false / [lastError] を立て、
 * 読み書きは空の捨てストアとして振る舞う。呼び出し側は [usable] を見てエラーを表面化できる。
 *
 * [migrate] は既存の平文 `SharedPreferences("chain_proximity")` から値を読み出し、
 * 暗号化ストアへコピーしたうえで平文側を削除する。冪等。
 *
 * 平文側 (`chain_proximity`) は読み取りと移行後の削除にのみ使い、新しい機密値を書かない。
 */
class SecureStore private constructor(
    private val prefs: SharedPreferences?,
    val lastError: String?,
) {
    /** Keystore が使えず捨てストアになっている場合は false。 */
    val usable: Boolean get() = prefs != null

    fun getString(key: String): String? = prefs?.getString(key, null)

    fun putString(key: String, value: String) {
        prefs?.edit()?.putString(key, value)?.commit()
    }

    fun getLong(key: String, defaultValue: Long = 0L): Long =
        prefs?.getLong(key, defaultValue) ?: defaultValue

    fun putLong(key: String, value: Long) {
        prefs?.edit()?.putLong(key, value)?.commit()
    }

    fun remove(key: String) {
        prefs?.edit()?.remove(key)?.commit()
    }

    fun clear() {
        prefs?.edit()?.clear()?.commit()
    }

    /**
     * 既存の平文ストアから機密値を 1 回だけ移行する。
     * 平文に残っているエントリだけを暗号化ストアへコピーし、コピーに成功したら平文から消す。
     * 平文が空なら何もしない (冪等)。
     */
    fun migrate(context: Context) {
        val store = prefs ?: return
        val legacy = context.getSharedPreferences(LEGACY_PREFS, Context.MODE_PRIVATE)
        val entries = legacy.all
        if (entries.isEmpty()) return

        val secureEditor = store.edit()
        for ((key, value) in entries) {
            when (value) {
                is String -> secureEditor.putString(key, value)
                is Set<*> -> secureEditor.putStringSet(key, value as Set<String>)
                is Int -> secureEditor.putInt(key, value)
                is Long -> secureEditor.putLong(key, value)
                is Float -> secureEditor.putFloat(key, value)
                is Boolean -> secureEditor.putBoolean(key, value)
            }
        }
        // 暗号化ストアへの書き込みが確定してからのみ平文を消す。
        // commit が失敗したら平文を残し、次回の呼び出しで再試行できるようにする (データを失わない)。
        if (!secureEditor.commit()) return

        val legacyEditor = legacy.edit()
        for (key in entries.keys) {
            legacyEditor.remove(key)
        }
        legacyEditor.commit()
    }

    companion object {
        /** 既存の平文ストア。読み取りと移行後の削除にのみ使う。 */
        const val LEGACY_PREFS = "chain_proximity"

        /** Keystore 鍵で保護する保存先ファイル。 */
        const val ENCRYPTED_PREFS = "chain_proximity_encrypted"

        fun create(context: Context): SecureStore {
            return try {
                val masterKey = MasterKey.Builder(context)
                    .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
                    .build()
                val prefs = EncryptedSharedPreferences.create(
                    context,
                    ENCRYPTED_PREFS,
                    masterKey,
                    EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
                    EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
                )
                SecureStore(prefs, null)
            } catch (e: Exception) {
                // 平文 fallback はしない。生成に失敗したら捨てストアとして振る舞い、
                // lastError で呼び出し側に通知する。
                SecureStore(null, e.message ?: e.javaClass.simpleName)
            }
        }
    }
}
