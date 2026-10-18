package jp.i32.chain.photos

import android.Manifest
import android.content.ContentUris
import android.content.ContentValues
import android.database.ContentObserver
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.provider.MediaStore
import android.util.Log
import com.getcapacitor.JSObject
import com.getcapacitor.PermissionState
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import com.getcapacitor.annotation.Permission
import com.getcapacitor.annotation.PermissionCallback
import java.io.BufferedInputStream
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.time.Instant
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
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
        private const val TAG = "ChainPhotos"
        private const val PREFS = "chain.photos"
        private const val SINCE = "since"
        private const val PENDING = "pending"
        private const val SEEN = "seen"
        private const val CURSOR_DATE = "cursorDate"
        private const val CURSOR_ID = "cursorId"
        private const val TEMP_DIR = "chain-photos"
        private const val MAX_BYTES = 15 * 1024 * 1024
        private val PASS_THROUGH_MIMES = setOf("image/jpeg", "image/png", "image/webp")
    }

    private val handler = Handler(Looper.getMainLooper())
    private val scanExecutor: ExecutorService = Executors.newSingleThreadExecutor()
    private val scanQueued = AtomicBoolean(false)
    private val recentUris = RecentUriSet()

    @Volatile
    private var observer: ContentObserver? = null

    // seen / pending はメモリに持ち、変わったときだけ永続化する。アクセスは stateLock 内で行う。
    private val stateLock = Any()
    private var seen: MutableSet<String>? = null
    private var pending: MutableList<JSONObject>? = null

    private val scanTrigger = Runnable { enqueueScan() }

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
        synchronized(stateLock) {
            val prefs = context.getSharedPreferences(PREFS, 0)
            if (prefs.getLong(SINCE, Long.MIN_VALUE) != sinceMs) {
                // 新しい since: cursor と seen を捨てる。
                prefs.edit().putLong(SINCE, sinceMs).putString(SEEN, "[]")
                    .remove(CURSOR_DATE).remove(CURSOR_ID).apply()
                seen = mutableSetOf()
            }
        }
        startObserver()
        try {
            scanExecutor.execute {
                cleanupTempFiles()
                runScan()
            }
        } catch (e: Exception) {
            Log.w(TAG, "scan を始められません", e)
        }
        call.resolve()
    }

    @PluginMethod
    fun stopWatching(call: PluginCall) {
        stopObserver()
        call.resolve()
    }

    @PluginMethod
    fun listPending(call: PluginCall) {
        val array = synchronized(stateLock) {
            JSONArray().also { array -> loadPending().forEach(array::put) }
        }
        call.resolve(JSObject().put("items", array))
    }

    @PluginMethod
    fun removePending(call: PluginCall) {
        val ids = call.getArray("ids")?.toList<String>()?.toSet() ?: emptySet()
        synchronized(stateLock) {
            val list = loadPending()
            val changed = list.removeAll { ids.contains(it.optString("mediaStoreId")) }
            val seenSet = loadSeen()
            val seenChanged = seenSet.addAll(ids)
            if (changed) persistPending()
            if (seenChanged) persistSeen()
        }
        call.resolve()
    }

    @PluginMethod
    fun readPhoto(call: PluginCall) {
        val id = call.getString("mediaStoreId") ?: return call.reject("mediaStoreId is required")
        val idLong = id.toLongOrNull() ?: return call.reject("invalid mediaStoreId")
        val uri = ContentUris.withAppendedId(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, idLong)
        try {
            scanExecutor.execute {
                try {
                    val result = copyToTemp(idLong, uri)
                    handler.post { call.resolve(result) }
                } catch (e: Exception) {
                    handler.post { call.reject(e.message ?: "画像を読み込めません", "read_failed") }
                }
            }
        } catch (e: Exception) {
            call.reject(e.message ?: "画像を読み込めません", "read_failed")
        }
    }

    @PluginMethod
    fun releasePhoto(call: PluginCall) {
        val path = call.getString("path") ?: return call.reject("path is required")
        if (!isReleasablePath(tempDir(), path)) {
            return call.reject("path is outside the photo cache", "invalid_path")
        }
        try {
            File(path).delete()
            call.resolve()
        } catch (e: Exception) {
            call.reject(e.message ?: "一時ファイルを消せません", "release_failed")
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
                // 自分で作った写真の通知は監視側で無視する。
                recentUris.add(uri.toString(), SystemClock.elapsedRealtime())
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

    // ---- observer / debounce ----

    private fun startObserver() {
        synchronized(this) {
            if (observer != null) return
            val next = object : ContentObserver(handler) {
                override fun onChange(selfChange: Boolean) {
                    scheduleScan()
                }

                override fun onChange(selfChange: Boolean, uri: Uri?) {
                    if (uri != null && isOwnInsert(uri)) return
                    scheduleScan()
                }

                // API 30+ はまとめて通知が来る。全部が自分の insert なら無視する。
                override fun onChange(selfChange: Boolean, uris: Collection<Uri>, flags: Int) {
                    if (uris.isNotEmpty() && uris.all { isOwnInsert(it) }) return
                    scheduleScan()
                }
            }
            observer = next
            context.contentResolver.registerContentObserver(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, true, next)
        }
    }

    private fun stopObserver() {
        synchronized(this) {
            observer?.let {
                try {
                    context.contentResolver.unregisterContentObserver(it)
                } catch (e: Exception) {
                    Log.w(TAG, "observer を外せません", e)
                }
            }
            observer = null
        }
        handler.removeCallbacks(scanTrigger)
    }

    private fun isOwnInsert(uri: Uri): Boolean = recentUris.contains(uri.toString(), SystemClock.elapsedRealtime())

    /** 通知のたびに待ち時間を延ばし、静かになってから 1 回だけ scan する。 */
    private fun scheduleScan() {
        try {
            handler.removeCallbacks(scanTrigger)
            handler.postAtTime(scanTrigger, debounceDeadline(SystemClock.uptimeMillis()))
        } catch (e: Exception) {
            Log.w(TAG, "scan を予約できません", e)
        }
    }

    /** scan は単一スレッドで走らせ、待ち行列には最大 1 件だけ積む。 */
    private fun enqueueScan() {
        if (!scanQueued.compareAndSet(false, true)) return
        try {
            scanExecutor.execute {
                scanQueued.set(false)
                runScan()
            }
        } catch (e: Exception) {
            scanQueued.set(false)
            Log.w(TAG, "scan を始められません", e)
        }
    }

    // ---- scan ----

    /** executor 上で呼ぶ。例外は外へ出さず watchError に変える。 */
    private fun runScan() {
        try {
            if (permissionStatus().getString(PHOTOS) != "granted") {
                onPermissionRevoked()
                return
            }
            scan()
        } catch (e: SecurityException) {
            Log.w(TAG, "写真の権限がありません", e)
            onPermissionRevoked()
        } catch (e: Exception) {
            Log.w(TAG, "scan に失敗しました", e)
            emitWatchError("query_failed")
        }
    }

    private fun onPermissionRevoked() {
        stopObserver()
        emitWatchError("permission_revoked")
    }

    private fun emitWatchError(reason: String) {
        try {
            notifyListeners("watchError", JSObject().put("reason", reason))
        } catch (e: Exception) {
            Log.w(TAG, "watchError を通知できません", e)
        }
    }

    private fun scan() {
        val sdk = Build.VERSION.SDK_INT
        val prefs = context.getSharedPreferences(PREFS, 0)
        val since = prefs.getLong(SINCE, 0L)
        val floor = sinceFloorSeconds(since)
        var cursorDate: Long
        var cursorId: Long
        synchronized(stateLock) {
            cursorDate = prefs.getLong(CURSOR_DATE, floor)
            cursorId = prefs.getLong(CURSOR_ID, 0L)
        }
        val startDate = cursorDate
        val startId = cursorId

        val pathColumn = if (sdk >= Build.VERSION_CODES.Q) MediaStore.Images.Media.RELATIVE_PATH else MediaStore.Images.Media.DATA
        val projection = arrayOf(
            MediaStore.Images.Media._ID,
            MediaStore.Images.Media.DATE_TAKEN,
            MediaStore.Images.Media.DATE_ADDED,
            pathColumn,
        )
        // IS_PENDING の行が後から確定したり、未来の takenAt で見送った行を拾い直せるよう、cursor より少し手前から重ねて検索する。
        // 二重登録は seen / pending で防ぐ。
        val lowerBound = rescanLowerBoundSeconds(startDate, floor)
        val selection = "${MediaStore.Images.Media.DATE_ADDED} >= ?"
        val args = arrayOf(lowerBound.toString())
        val sortOrder = "${MediaStore.Images.Media.DATE_ADDED} ASC, ${MediaStore.Images.Media._ID} ASC"
        val now = System.currentTimeMillis()

        val cursor = context.contentResolver.query(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, projection, selection, args, sortOrder)
            ?: throw IllegalStateException("MediaStore を検索できません")
        cursor.use {
            val idIndex = it.getColumnIndexOrThrow(MediaStore.Images.Media._ID)
            val takenIndex = it.getColumnIndexOrThrow(MediaStore.Images.Media.DATE_TAKEN)
            val addedIndex = it.getColumnIndexOrThrow(MediaStore.Images.Media.DATE_ADDED)
            val pathIndex = it.getColumnIndexOrThrow(pathColumn)
            while (it.moveToNext()) {
                val rowId = it.getLong(idIndex)
                val added = it.getLong(addedIndex)
                if (isAfterCursor(added, rowId, cursorDate, cursorId)) {
                    cursorDate = added
                    cursorId = rowId
                }
                val path = if (it.isNull(pathIndex)) null else it.getString(pathIndex)
                val inFolder = if (sdk >= Build.VERSION_CODES.Q) isInCameraFolder(sdk, path, null) else isInCameraFolder(sdk, null, path)
                if (!inFolder) continue
                val taken = takenAtMillis(it.getLong(takenIndex), added)
                if (!isWithinWindow(taken, since, now)) continue
                addPending(rowId.toString(), taken)
            }
        }
        if (cursorDate != startDate || cursorId != startId) {
            synchronized(stateLock) {
                // 途中で since が変わっていたら cursor を上書きしない。
                if (prefs.getLong(SINCE, 0L) == since) {
                    prefs.edit().putLong(CURSOR_DATE, cursorDate).putLong(CURSOR_ID, cursorId).apply()
                }
            }
        }
    }

    private fun addPending(id: String, takenAt: Long) {
        val takenIso = Instant.ofEpochMilli(takenAt).toString()
        val uri = ContentUris.withAppendedId(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, id.toLong()).toString()
        val foundIso = Instant.now().toString()
        val item = JSONObject().put("mediaStoreId", id).put("takenAt", takenIso).put("uri", uri).put("foundAt", foundIso)
        synchronized(stateLock) {
            if (loadSeen().contains(id)) return
            val list = loadPending()
            if (list.any { it.optString("mediaStoreId") == id }) return
            list.add(item)
            persistPending()
        }
        notifyListeners(
            "photoCaptured",
            JSObject().put("mediaStoreId", id).put("takenAt", takenIso).put("uri", uri).put("foundAt", foundIso),
        )
    }

    // ---- persisted state (call with stateLock held) ----

    private fun loadPending(): MutableList<JSONObject> {
        pending?.let { return it }
        val prefs = context.getSharedPreferences(PREFS, 0)
        val list = mutableListOf<JSONObject>()
        try {
            val array = JSONArray(prefs.getString(PENDING, "[]") ?: "[]")
            for (i in 0 until array.length()) {
                val item = array.getJSONObject(i)
                if (!item.has("foundAt")) item.put("foundAt", item.optString("takenAt"))
                list.add(item)
            }
        } catch (e: Exception) {
            Log.w(TAG, "pending が壊れているので空に戻します", e)
            list.clear()
            prefs.edit().putString(PENDING, "[]").apply()
        }
        pending = list
        return list
    }

    private fun loadSeen(): MutableSet<String> {
        seen?.let { return it }
        val prefs = context.getSharedPreferences(PREFS, 0)
        val set = mutableSetOf<String>()
        try {
            val array = JSONArray(prefs.getString(SEEN, "[]") ?: "[]")
            for (i in 0 until array.length()) set.add(array.getString(i))
        } catch (e: Exception) {
            Log.w(TAG, "seen が壊れているので空に戻します", e)
            set.clear()
            prefs.edit().putString(SEEN, "[]").apply()
        }
        seen = set
        return set
    }

    private fun persistPending() {
        val array = JSONArray()
        loadPending().forEach { array.put(it) }
        context.getSharedPreferences(PREFS, 0).edit().putString(PENDING, array.toString()).apply()
    }

    private fun persistSeen() {
        val array = JSONArray()
        loadSeen().forEach { array.put(it) }
        context.getSharedPreferences(PREFS, 0).edit().putString(SEEN, array.toString()).apply()
    }

    // ---- readPhoto / temp files ----

    private fun tempDir(): File = File(context.cacheDir, TEMP_DIR)

    private fun cleanupTempFiles() {
        try {
            val now = System.currentTimeMillis()
            tempDir().listFiles()?.forEach { if (isStaleTempFile(it.lastModified(), now)) it.delete() }
        } catch (e: Exception) {
            Log.w(TAG, "一時ファイルを掃除できません", e)
        }
    }

    /** executor 上で呼ぶ。必要なら変換して cache に書き、path / mime / size を返す。 */
    private fun copyToTemp(id: Long, uri: Uri): JSObject {
        val mime = context.contentResolver.getType(uri) ?: "image/jpeg"
        val bytes = context.contentResolver.openInputStream(uri)?.use { input ->
            val data = input.readBytes()
            if (mime in PASS_THROUGH_MIMES && data.size <= MAX_BYTES) data else compressForUpload(data)
        } ?: throw IllegalStateException("画像を読み込めません")
        val outputMime = if (mime in PASS_THROUGH_MIMES && bytes.size <= MAX_BYTES) mime else "image/jpeg"
        val dir = tempDir()
        if (!dir.isDirectory && !dir.mkdirs()) throw IllegalStateException("一時フォルダを作れません")
        // 同じ id の古い一時ファイルは拡張子違いも含めて消す。
        dir.listFiles()?.filter { it.nameWithoutExtension == id.toString() }?.forEach { it.delete() }
        val file = File(dir, "$id.${extensionForMime(outputMime)}")
        file.outputStream().use { it.write(bytes) }
        return JSObject().put("path", file.absolutePath).put("mime", outputMime).put("size", bytes.size)
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
}
