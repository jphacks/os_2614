@file:Suppress("DEPRECATION", "UNCHECKED_CAST")

package jp.i32.chain.proximity

import android.content.Context
import android.content.SharedPreferences
import android.util.Log
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

/**
 * token や時刻補正のような機密値を Android Keystore の鍵で暗号化して保存する薄いラッパ。
 *
 * 保存先は [EncryptedSharedPreferences] (`chain_proximity_encrypted`)。キーは AES256_SIV、
 * 値は AES256_GCM で暗号化される。**この API は security-crypto 1.1.0 で非推奨** だが、
 * 仕様 (`specification/android/README.md:92`) が明示しているため採用している。移行先 (exit path) は
 * `androidx.datastore` など非推奨でない暗号化ストレージが整い次第、この公開 API
 * ([getString] / [putString] / [getLong] / [putLong] / [remove] / [clear] / [migrate]) の裏側を
 * 差し替える。非推奨警告はファイル先頭の `@file:Suppress("DEPRECATION")` で抑止する。
 *
 * [EncryptedSharedPreferences] の生成に失敗した場合 (端末の Keystore 異常など) は
 * **平文への fallback を一切しない**。例外を握って [usable] = false / [lastError] を立て、
 * 読み書きは空の捨てストアとして振る舞う。呼び出し側は [usable] を見てエラーを表面化できる。
 *
 * [migrate] は既存の平文 `SharedPreferences("chain_proximity")` から値を読み出し、暗号化ストアへ
 * コピーしたうえで平文側を削除する冪等な移行。
 *  - 暗号化ストアに既に値があるキーは上書きしない (古い平文で新しい暗号化値を壊さない)。
 *  - コピーできなかったキー (未知型) は平文に残す。平文からの削除は「暗号化側へ移ったキー」だけ。
 *  - 暗号化ストアへの永続化が確定してからのみ平文を消す。平文の削除に失敗しても暗号化側のコピーは
 *    失われず、次回の [migrate] で再削除される。
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
        // 通常の書き込みは fire-and-forget の apply でよい: 読み手は同じ in-memory マップを
        // 即座に見るため往復は保たれる。main thread を同期 fsync で塞がない (F-07)。
        // 戻り値が要るのは移行経路だけなので、同期書き込みは [migrate] に限定する。
        prefs?.edit()?.putString(key, value)?.apply()
    }

    fun getLong(key: String, defaultValue: Long = 0L): Long =
        prefs?.getLong(key, defaultValue) ?: defaultValue

    fun putLong(key: String, value: Long) {
        // apply: in-memory は即時反映、ディスクは非同期 (F-07)。
        prefs?.edit()?.putLong(key, value)?.apply()
    }

    fun remove(key: String) {
        prefs?.edit()?.remove(key)?.apply()
    }

    fun clear() {
        prefs?.edit()?.clear()?.apply()
    }

    /**
     * 既存の平文ストアから機密値を 1 回だけ移行する (冪等)。
     * 平文に残っているエントリのうち暗号化ストアにまだ無いキーだけをコピーし、暗号化側へ移った
     * キー (コピー済み + 既存で上書きしなかった分) を平文から消す。
     */
    fun migrate(context: Context) {
        val store = prefs ?: return
        val legacy = context.getSharedPreferences(LEGACY_PREFS, Context.MODE_PRIVATE)
        val entries = legacy.all
        if (entries.isEmpty()) return

        val secureEditor = store.edit()
        // 実際に暗号化ストアへ書き込んだキー (平文から消してよい)。
        val copied = mutableListOf<String>()
        // 暗号化ストアに既存だったキー。平文側の古い値は破棄してよい (暗号化側が新しい)。
        val superseded = mutableListOf<String>()
        for ((key, value) in entries) {
            if (store.contains(key)) {
                // F-03: 暗号化ストアの値の方が新しい。古い平文で上書きしない。
                superseded += key
                continue
            }
            val written = when (value) {
                is String -> {
                    secureEditor.putString(key, value)
                    true
                }
                is Set<*> -> {
                    secureEditor.putStringSet(key, value as Set<String>)
                    true
                }
                is Int -> {
                    secureEditor.putInt(key, value)
                    true
                }
                is Long -> {
                    secureEditor.putLong(key, value)
                    true
                }
                is Float -> {
                    secureEditor.putFloat(key, value)
                    true
                }
                is Boolean -> {
                    secureEditor.putBoolean(key, value)
                    true
                }
                // 未知型はコピーしない -> 平文からも消さない (F-03 の silent loss 防止)。
                else -> false
            }
            if (written) copied += key
        }

        // 移行経路だけが同期書き込みの戻り値を見る (平文を消してよいかが戻り値に依存するため)。
        // 失敗したら平文を残し、次回の migrate で再試行する (データを失わない)。
        if (!secureEditor.commit()) return

        // 平文から消してよいのは「暗号化側へ移った」キーだけ。未知型のキーは平文に残す。
        val removable = copied + superseded
        if (removable.isEmpty()) return
        val removalEditor = legacy.edit()
        for (key in removable) removalEditor.remove(key)
        // 平文の削除に失敗しても暗号化側のコピーは既に永続化済みなのでデータは失われない。
        // 失敗を握りつぶさず記録し、次回の migrate で再削除する (冪等)。
        if (!removalEditor.commit()) {
            Log.w(TAG, "legacy plaintext cleanup failed; retrying on next migrate")
        }
    }

    companion object {
        private const val TAG = "SecureStore"

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
