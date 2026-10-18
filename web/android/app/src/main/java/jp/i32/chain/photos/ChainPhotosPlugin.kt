package jp.i32.chain.photos

import android.content.ContentUris
import android.content.ContentValues
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.MediaStore
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import java.io.BufferedInputStream
import java.net.HttpURLConnection
import java.net.URL
import java.time.Instant

/** パーティの写真を端末のアルバムへ保存するだけのプラグイン。Android 10+ の MediaStore は権限なしで書ける。 */
@CapacitorPlugin(name = "ChainPhotos")
class ChainPhotosPlugin : Plugin() {
    private val handler = Handler(Looper.getMainLooper())

    @PluginMethod
    fun savePhoto(call: PluginCall) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
            return call.reject("Android 10 以降でのみ利用できます", "unavailable")
        }
        val url = call.getString("url") ?: return call.reject("url is required")
        val album = call.getString("album") ?: "party"
        val safeAlbum = sanitizeAlbum(album)
        val fileName = (call.getString("fileName") ?: "photo.jpg").replace(Regex("[^A-Za-z0-9._-]"), "_")
        val takenAt = call.getString("takenAt")?.let { runCatching { Instant.parse(it).toEpochMilli() }.getOrNull() }
        Thread {
            try {
                val connection = URL(url).openConnection() as HttpURLConnection
                connection.connectTimeout = 15_000
                connection.readTimeout = 30_000
                connection.connect()
                if (connection.responseCode !in 200..299) throw IllegalStateException("画像を取得できません")
                val values = ContentValues().apply {
                    put(MediaStore.Images.Media.DISPLAY_NAME, fileName)
                    put(MediaStore.Images.Media.MIME_TYPE, "image/jpeg")
                    put(MediaStore.Images.Media.RELATIVE_PATH, "Pictures/CHAIN/$safeAlbum")
                    takenAt?.let { put(MediaStore.Images.Media.DATE_TAKEN, it) }
                    put(MediaStore.Images.Media.IS_PENDING, 1)
                }
                val uri = context.contentResolver.insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values)
                    ?: throw IllegalStateException("保存先を作れません")
                try {
                    context.contentResolver.openOutputStream(uri)?.use { output ->
                        BufferedInputStream(connection.inputStream).use { input -> input.copyTo(output) }
                    } ?: throw IllegalStateException("保存先を開けません")
                    values.clear()
                    values.put(MediaStore.Images.Media.IS_PENDING, 0)
                    context.contentResolver.update(uri, values, null, null)
                    val result = JSObject().put("mediaStoreId", ContentUris.parseId(uri).toString())
                    handler.post { call.resolve(result) }
                } catch (e: Exception) {
                    context.contentResolver.delete(uri, null, null)
                    throw e
                } finally {
                    connection.disconnect()
                }
            } catch (e: Exception) {
                handler.post { call.reject(e.message ?: "写真を保存できません", "save_failed") }
            }
        }.start()
    }

    private fun sanitizeAlbum(value: String): String {
        val safe = value.filter { it.code >= 0x20 && it != '/' && it != '\\' && it != ':' }
            .replace("..", "_").trim()
        return safe.ifEmpty { "party" }
    }
}
