@file:Suppress("DEPRECATION")

package jp.i32.chain.proximity

import android.content.Context
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * instrumented test for [SecureStore]: Keystore-backed round-trip, no plaintext residue,
 * and one-time migration off the legacy plaintext `SharedPreferences("chain_proximity")`.
 */
@RunWith(AndroidJUnit4::class)
class SecureStoreTest {

    private lateinit var context: Context

    @Before
    fun setUp() {
        context = InstrumentationRegistry.getInstrumentation().targetContext
        clearStores()
    }

    @After
    fun tearDown() {
        clearStores()
    }

    private fun clearStores() {
        context.getSharedPreferences(SecureStore.LEGACY_PREFS, Context.MODE_PRIVATE)
            .edit().clear().commit()
        SecureStore.create(context).clear()
    }

    @Test
    fun roundTripSurvivesFreshEncryptedHandle() {
        val first = SecureStore.create(context)
        assertTrue("Keystore が使える端末で実行すること (usable=${first.usable}, lastError=${first.lastError})", first.usable)

        first.putString(KEY_TOKENS, "VALUE-ONE")
        first.putLong(KEY_OFFSET, 12_345L)

        // 新しい EncryptedSharedPreferences ハンドル (別インスタンス) で読み直す。
        val fresh = SecureStore.create(context)
        assertEquals("VALUE-ONE", fresh.getString(KEY_TOKENS))
        assertEquals(12_345L, fresh.getLong(KEY_OFFSET, -1L))
        // 未設定キーは default に落ちる。
        assertEquals(-7L, fresh.getLong("missing", -7L))

        // ディスク上の暗号化ファイルが存在し、平文が書かれていないこと (ciphertext 保存の証明)。
        val encFile = encryptedPrefsFile()
        assertTrue("encrypted prefs file should exist: ${encFile.absolutePath}", encFile.exists())
        assertFalse("encrypted xml must not contain plaintext value", encFile.readText().contains("VALUE-ONE"))
    }

    @Test
    fun writeLeavesNoPlaintextResidue() {
        val store = SecureStore.create(context)
        assertTrue(store.usable)
        store.putString(KEY_TOKENS, SECRET)

        // 暗号化ストアからは読める (値が本当に保存されている)。
        assertEquals(SECRET, SecureStore.create(context).getString(KEY_TOKENS))

        // 平文 prefs は空のまま (SecureStore は平文側へ書かない)。
        val plaintext = context.getSharedPreferences(SecureStore.LEGACY_PREFS, Context.MODE_PRIVATE)
        assertTrue("legacy plaintext prefs must stay empty: ${plaintext.all}", plaintext.all.isEmpty())

        // shared_prefs/ 配下の XML に平文が現れないこと (平文ファイルにも暗号化ファイルにも)。
        val leaks = sharedPrefsFiles().filter { it.readText().contains(SECRET) }.map { it.name }
        assertTrue("plaintext residue leaked into: $leaks", leaks.isEmpty())
    }

    @Test
    fun migrateMovesLegacyValuesAndIsIdempotent() {
        // 既存の平文ストアを再現する。
        val legacy = context.getSharedPreferences(SecureStore.LEGACY_PREFS, Context.MODE_PRIVATE)
        legacy.edit()
            .putString(KEY_TOKENS, LEGACY_TOKENS)
            .putLong(KEY_WINDOW, 900L)
            .putLong(KEY_OFFSET, 4_242L)
            .commit()

        SecureStore.create(context).migrate(context)

        // 暗号化ストアから読める。
        val migrated = SecureStore.create(context)
        assertEquals(LEGACY_TOKENS, migrated.getString(KEY_TOKENS))
        assertEquals(900L, migrated.getLong(KEY_WINDOW, -1L))
        assertEquals(4_242L, migrated.getLong(KEY_OFFSET, -1L))

        // 平文側からは消えている。
        assertTrue("legacy prefs after migrate: ${legacy.all}", legacy.all.isEmpty())
        val legacyFile = File(sharedPrefsDir(), "${SecureStore.LEGACY_PREFS}.xml")
        assertFalse(
            "legacy xml must not hold the migrated value",
            legacyFile.exists() && legacyFile.readText().contains(LEGACY_TOKENS),
        )

        // 2 回目は no-op (冪等)。値は変わらず、平文も空のまま。
        migrated.migrate(context)
        val again = SecureStore.create(context)
        assertEquals(LEGACY_TOKENS, again.getString(KEY_TOKENS))
        assertEquals(900L, again.getLong(KEY_WINDOW, -1L))
        assertEquals(4_242L, again.getLong(KEY_OFFSET, -1L))
        assertTrue(legacy.all.isEmpty())
    }

    @Test
    fun migrateDoesNotClobberNewerEncryptedValue() {
        // 前回の移行で平文削除が失敗し、古い平文が残っている状況を再現する (F-03)。
        val legacy = context.getSharedPreferences(SecureStore.LEGACY_PREFS, Context.MODE_PRIVATE)
        legacy.edit()
            .putString(KEY_TOKENS, STALE_PLAINTEXT_TOKENS)
            .putLong(KEY_OFFSET, 1L)
            .putLong(KEY_WINDOW, 900L)
            .commit()

        // その後、より新しい token/offset が暗号化ストアへ書かれた (平文より新しい)。
        val secure = SecureStore.create(context)
        assertTrue(secure.usable)
        secure.putString(KEY_TOKENS, FRESH_ENCRYPTED_TOKENS)
        secure.putLong(KEY_OFFSET, 99_999L)

        // 次回の load() 相当。古い平文で新しい暗号化値を上書きしてはならない (F-03)。
        SecureStore.create(context).migrate(context)

        val after = SecureStore.create(context)
        assertEquals(
            "newer encrypted token must survive migrate()",
            FRESH_ENCRYPTED_TOKENS,
            after.getString(KEY_TOKENS),
        )
        assertEquals(99_999L, after.getLong(KEY_OFFSET, -1L))
        // 暗号化側に無かったキーは (古い平文から) 正しくコピーされる。
        assertEquals(900L, after.getLong(KEY_WINDOW, -1L))

        // 古い平文エントリは消える (暗号化側が正なので二重管理を残さない)。
        assertFalse("stale plaintext tokens must be cleared", legacy.contains(KEY_TOKENS))
        assertFalse("stale plaintext offset must be cleared", legacy.contains(KEY_OFFSET))
        assertFalse("copied key must be cleared from plaintext", legacy.contains(KEY_WINDOW))
    }

    private fun sharedPrefsDir(): File = File(context.dataDir, "shared_prefs")

    private fun sharedPrefsFiles(): List<File> =
        sharedPrefsDir().listFiles().orEmpty().filter { it.isFile && it.name.endsWith(".xml") }

    private fun encryptedPrefsFile(): File =
        File(sharedPrefsDir(), "${SecureStore.ENCRYPTED_PREFS}.xml")

    private companion object {
        const val KEY_TOKENS = "tokens"
        const val KEY_WINDOW = "window_sec"
        const val KEY_OFFSET = "clock_offset_ms"
        const val SECRET = "SECRET"
        const val LEGACY_TOKENS = "TOKENS-JSON"
        const val STALE_PLAINTEXT_TOKENS = "STALE-PLAINTEXT"
        const val FRESH_ENCRYPTED_TOKENS = "FRESH-ENCRYPTED"
    }
}
