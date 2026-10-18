package jp.i32.chain.photos

import android.Manifest
import android.content.ContentUris
import android.content.ContentValues
import android.database.ContentObserver
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.MediaStore
import android.util.Base64
import com.getcapacitor.JSObject
import com.getcapacitor.PermissionState
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import com.getcapacitor.annotation.Permission
import com.getcapacitor.annotation.PermissionCallback
import java.io.BufferedInputStream
import java.net.HttpURLConnection
import java.net.URL
import java.time.Instant
import java.util.concurrent.Executors
import java.util.concurrent.ExecutorService
import org.json.JSONArray
import org.json.JSONObject

/**
 * 写真権限の状態を Android の版から決める（JS へ返す値: granted / denied / prompt / prompt-with-rationale / limited）。
 * [fallback] は granted でも limited でもないときに返す、版に応じた alias の状態。
 */
internal fun photoPermission(sdk: Int, imagesGranted: Boolean, selectedGranted: Boolean, fallback: String): String =
    when {
        sdk >= 33 && imagesGranted -> "granted"
        sdk >= 34 && selectedGranted -> "limited"
        else -> fallback
    }

/** 版ごとに要求する alias。34+ は部分アクセスを選べるよう mediaSelected も一緒に要求する。 */
internal fun requestAliases(sdk: Int): Array<String> = when {
    sdk >= 34 -> arrayOf(ChainPhotosPlugin.MEDIA_IMAGES, ChainPhotosPlugin.MEDIA_SELECTED)
    sdk >= 33 -> arrayOf(ChainPhotosPlugin.MEDIA_IMAGES)
    else -> arrayOf(ChainPhotosPlugin.STORAGE)
}

/** 写真の監視・端末保存だけを担当するプラグイン。backend 通信は WebView が行う。 */
@CapacitorPlugin(
    name = "ChainPhotos",
    permissions = [
        // 1 つの alias に束ねると Capacitor は「全部許可」でないと GRANTED にしない。
        // Android 13+ では READ_EXTERNAL_STORAGE が決して許可されないので、版ごとに alias を分ける。
        Permission(alias = ChainPhotosPlugin.MEDIA_IMAGES, strings = [Manifest.permission.READ_MEDIA_IMAGES]),
        Permission(alias = ChainPhotosPlugin.MEDIA_SELECTED, strings = [Manifest.permission.READ_MEDIA_VISUAL_USER_SELECTED]),
        Permission(alias = ChainPhotosPlugin.STORAGE, strings = [Manifest.permission.READ_EXTERNAL_STORAGE]),
    ],
)
class ChainPhotosPlugin : Plugin() {
    companion object {
        const val PHOTOS = "photos"
        const val MEDIA_IMAGES = "mediaImages"
        const val MEDIA_SELECTED = "mediaSelected"
        const val STORAGE = "storage"
        private const val PREFS = "chain.photos"
        private const val SINCE = "since"
        private const val PENDING = "pending"
        private const val SEEN = "seen"
        private const val MAX_BYTES = 15 * 1024 * 1024
    }

    private val handler = Handler(Looper.getMainLooper())
    private val scanExecutor: ExecutorService = Executors.newSingleThreadExecutor()
    private var observer: ContentObserver? = null

    override fun handleOnDestroy() {
        stopObserver()
        scanExecutor.shutdownNow()
        super.handleOnDestroy()
    }

    @PluginMethod
    override fun checkPermissions(call: PluginCall) {
        call.resolve(permissionStatus())
    }

    @PluginMethod
    override fun requestPermissions(call: PluginCall) {
        requestPermissionForAliases(requestAliases(Build.VERSION.SDK_INT), call, "permissionsCallback")
    }

    @PermissionCallback
    private fun permissionsCallback(call: PluginCall) {
        call.resolve(permissionStatus())
    }

    private fun permissionStatus(): JSObject {
        val sdk = Build.VERSION.SDK_INT
        val status = JSObject()
        status.put(
            PHOTOS,
            photoPermission(
                sdk,
                imagesGranted = getPermissionState(MEDIA_IMAGES) == PermissionState.GRANTED,
                selectedGranted = getPermissionState(MEDIA_SELECTED) == PermissionState.GRANTED,
                fallback = getPermissionState(if (sdk >= 33) MEDIA_IMAGES else STORAGE).toString(),
            ),
        )
        return status
    }

    @PluginMethod
    fun startWatching(call: PluginCall) {
        val since = call.getString("since") ?: return call.reject("since is required")
        val sinceMs = try {
            Instant.parse(since).toEpochMilli()
        } catch (_: Exception) {
            return call.reject("since must be ISO 8601")
        }
        if (permissionStatus().getString(PHOTOS) != "granted") {
            return call.reject("写真の権限がありません", "denied")
        }
        synchronized(this) {
            val prefs = context.getSharedPreferences(PREFS, 0)
            if (prefs.getLong(SINCE, Long.MIN_VALUE) != sinceMs) {
                prefs.edit().putLong(SINCE, sinceMs).putString(SEEN, "[]").apply()
            } else {
                prefs.edit().putLong(SINCE, sinceMs).apply()
            }
        }
        startObserver()
        scanExecutor.execute { scan(sinceMs) }
        call.resolve()
    }

    @PluginMethod
    fun stopWatching(call: PluginCall) {
        stopObserver()
        call.resolve()
    }

    @PluginMethod
    fun listPending(call: PluginCall) {
        call.resolve(JSObject().put("items", readPending().toJSONArray()))
    }

    @PluginMethod
    fun removePending(call: PluginCall) {
        val ids = call.getArray("ids")?.toList<String>()?.toSet() ?: emptySet()
        synchronized(this) {
            val remaining = readPending().filterNot { ids.contains(it.getString("mediaStoreId")) }
            addSeen(ids)
            writePending(remaining)
        }
        call.resolve()
    }

    @PluginMethod
    fun readPhoto(call: PluginCall) {
        val id = call.getString("mediaStoreId") ?: return call.reject("mediaStoreId is required")
        val uri = ContentUris.withAppendedId(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, id.toLongOrNull() ?: return call.reject("invalid mediaStoreId"))
        try {
            val mime = context.contentResolver.getType(uri) ?: "image/jpeg"
            val bytes = context.contentResolver.openInputStream(uri)?.use { input ->
                val data = input.readBytes()
                if (mime in setOf("image/jpeg", "image/png", "image/webp") && data.size <= MAX_BYTES) {
                    data
                } else {
                    compressForUpload(data)
                }
            } ?: return call.reject("画像を読み込めません")
            val outputMime = if (mime in setOf("image/jpeg", "image/png", "image/webp") && bytes.size <= MAX_BYTES) mime else "image/jpeg"
            call.resolve(JSObject().put("data", Base64.encodeToString(bytes, Base64.NO_WRAP)).put("mime", outputMime))
        } catch (e: Exception) {
            call.reject(e.message ?: "画像を読み込めません", "read_failed")
        }
    }

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

    @PluginMethod
    fun isSaved(call: PluginCall) {
        val id = call.getString("mediaStoreId") ?: return call.reject("mediaStoreId is required")
        val saved = context.contentResolver.query(
            MediaStore.Images.Media.EXTERNAL_CONTENT_URI,
            arrayOf(MediaStore.Images.Media._ID),
            "${MediaStore.Images.Media._ID} = ?",
            arrayOf(id),
            null,
        )?.use { it.moveToFirst() } == true
        call.resolve(JSObject().put("saved", saved))
    }

    private fun startObserver() {
        if (observer != null) return
        val next = object : ContentObserver(handler) {
            override fun onChange(selfChange: Boolean) {
                super.onChange(selfChange)
                scanExecutor.execute {
                    scan(context.getSharedPreferences(PREFS, 0).getLong(SINCE, 0L))
                }
            }
        }
        observer = next
        context.contentResolver.registerContentObserver(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, true, next)
    }

    private fun stopObserver() {
        observer?.let { context.contentResolver.unregisterContentObserver(it) }
        observer = null
    }

    private fun scan(since: Long) {
        val projection = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            arrayOf(MediaStore.Images.Media._ID, MediaStore.Images.Media.DATE_TAKEN, "relative_path")
        } else {
            arrayOf(MediaStore.Images.Media._ID, MediaStore.Images.Media.DATE_TAKEN, MediaStore.Images.Media.DATA)
        }
        val selection = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) "date_taken >= ? AND date_taken <= ? AND relative_path LIKE ?" else "date_taken >= ? AND date_taken <= ? AND data LIKE ?"
        val now = System.currentTimeMillis()
        val args = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            arrayOf(since.toString(), now.toString(), "DCIM/Camera/%")
        } else {
            arrayOf(since.toString(), now.toString(), "%/DCIM/Camera/%")
        }
        context.contentResolver.query(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, projection, selection, args, "date_taken ASC")?.use { cursor ->
            val idIndex = cursor.getColumnIndexOrThrow(MediaStore.Images.Media._ID)
            val dateIndex = cursor.getColumnIndexOrThrow(MediaStore.Images.Media.DATE_TAKEN)
            while (cursor.moveToNext()) {
                val id = cursor.getLong(idIndex).toString()
                val taken = cursor.getLong(dateIndex)
                if (!isSeen(id)) addPending(id, taken)
            }
        }
    }

    private fun addPending(id: String, takenAt: Long) {
        val current = readPending()
        if (current.any { it.getString("mediaStoreId") == id }) return
        val item = JSONObject().put("mediaStoreId", id)
            .put("takenAt", Instant.ofEpochMilli(takenAt).toString())
            .put("uri", ContentUris.withAppendedId(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, id.toLong()).toString())
        synchronized(this) {
            if (isSeen(id)) return
            writePending(current + item)
        }
        notifyListeners(
            "photoCaptured",
            JSObject()
                .put("mediaStoreId", item.getString("mediaStoreId"))
                .put("takenAt", item.getString("takenAt"))
                .put("uri", item.getString("uri")),
        )
    }

    @Synchronized
    private fun readPending(): List<JSONObject> {
        val raw = context.getSharedPreferences(PREFS, 0).getString(PENDING, "[]") ?: "[]"
        val array = JSONArray(raw)
        return (0 until array.length()).map { array.getJSONObject(it) }
    }

    private fun isSeen(id: String): Boolean = synchronized(this) {
        val raw = context.getSharedPreferences(PREFS, 0).getString(SEEN, "[]") ?: "[]"
        val array = JSONArray(raw)
        (0 until array.length()).any { array.getString(it) == id }
    }

    private fun addSeen(ids: Set<String>) {
        val prefs = context.getSharedPreferences(PREFS, 0)
        val array = JSONArray(prefs.getString(SEEN, "[]") ?: "[]")
        val current = (0 until array.length()).map { array.getString(it) }.toMutableSet()
        ids.forEach { current.add(it) }
        val next = JSONArray()
        current.forEach { next.put(it) }
        prefs.edit().putString(SEEN, next.toString()).apply()
    }

    private fun sanitizeAlbum(value: String): String {
        val safe = value.filter { it.code >= 0x20 && it != '/' && it != '\\' && it != ':' }
            .replace("..", "_").trim()
        return safe.ifEmpty { "party" }
    }

    private fun compressForUpload(data: ByteArray): ByteArray {
        val source = BitmapFactory.decodeByteArray(data, 0, data.size)
            ?: throw IllegalArgumentException("画像をデコードできません")
        var bitmap = source
        val longest = maxOf(source.width, source.height)
        if (longest > 4096) {
            val scale = 4096f / longest
            bitmap = Bitmap.createScaledBitmap(source, (source.width * scale).toInt(), (source.height * scale).toInt(), true)
            source.recycle()
        }
        var quality = 90
        var result: ByteArray
        do {
            val output = java.io.ByteArrayOutputStream()
            bitmap.compress(Bitmap.CompressFormat.JPEG, quality, output)
            result = output.toByteArray()
            quality -= 10
        } while (result.size > MAX_BYTES && quality >= 40)
        bitmap.recycle()
        require(result.size <= MAX_BYTES) { "画像が大きすぎます" }
        return result
    }

    @Synchronized
    private fun writePending(items: List<JSONObject>) {
        val array = JSONArray()
        items.forEach { array.put(it) }
        context.getSharedPreferences(PREFS, 0).edit().putString(PENDING, array.toString()).apply()
    }

    private fun List<JSONObject>.toJSONArray(): JSONArray = JSONArray().also { array -> forEach(array::put) }
}
