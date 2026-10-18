package jp.i32.chain.photos

import java.io.File

/** 通知を受けてから scan を走らせるまでの待ち時間（通知が続けば延長する）。 */
internal const val SCAN_DEBOUNCE_MS = 1_500L

/** DATE_ADDED（秒）で since を拾うときに引く余裕。 */
internal const val SINCE_MARGIN_SECONDS = 60L

/** savePhoto で作った uri を覚えておく数と時間。 */
internal const val RECENT_URI_MAX = 50
internal const val RECENT_URI_TTL_MS = 5 * 60 * 1_000L

/** 一時ファイルを消すまでの経過時間。 */
internal const val TEMP_FILE_MAX_AGE_MS = 24 * 60 * 60 * 1_000L

/** 撮影時刻（ms）。DATE_TAKEN が空（0 以下）のときは DATE_ADDED（秒）で代用する。 */
internal fun takenAtMillis(dateTakenMs: Long, dateAddedSec: Long): Long =
    if (dateTakenMs > 0) dateTakenMs else dateAddedSec * 1000

/** since（ms）から、DATE_ADDED（秒）の下限を求める。負にはしない。 */
internal fun sinceFloorSeconds(sinceMs: Long): Long = maxOf(0L, sinceMs / 1000 - SINCE_MARGIN_SECONDS)

/** cursor から巻き戻して重ねて検索する幅（秒）。IS_PENDING で遅れて確定する行を拾い直すため。 */
internal const val RESCAN_OVERLAP_SECONDS = 120L

/** 検索の DATE_ADDED（秒）下限。cursor を [RESCAN_OVERLAP_SECONDS] 巻き戻すが、since の下限（[floorSec]）より下げない。 */
internal fun rescanLowerBoundSeconds(cursorDateSec: Long, floorSec: Long): Long =
    maxOf(floorSec, cursorDateSec - RESCAN_OVERLAP_SECONDS)

/** DCIM/Camera 直下か。29 以上は RELATIVE_PATH の先頭、未満は DATA の部分一致で見る。 */
internal fun isInCameraFolder(sdk: Int, relativePath: String?, data: String?): Boolean =
    if (sdk >= 29) {
        relativePath?.startsWith("DCIM/Camera/") == true
    } else {
        data?.contains("/DCIM/Camera/") == true
    }

/** 撮影時刻が [since, now] に入っているか。 */
internal fun isWithinWindow(takenAtMs: Long, sinceMs: Long, nowMs: Long): Boolean = takenAtMs in sinceMs..nowMs

/** 最後の変更通知の時刻から、scan を走らせる時刻（uptime ms）を求める。 */
internal fun debounceDeadline(nowMs: Long, delayMs: Long = SCAN_DEBOUNCE_MS): Long = nowMs + delayMs

/** releasePhoto で消してよいパスか。chain-photos ディレクトリの中身（正規化後）だけを許す。 */
internal fun isReleasablePath(baseDir: File, path: String): Boolean {
    if (path.isEmpty()) return false
    return try {
        val base = baseDir.canonicalPath
        val target = File(path).canonicalPath
        target.startsWith(base + File.separator)
    } catch (_: Exception) {
        false
    }
}

/** 出力 mime から一時ファイルの拡張子を決める。 */
internal fun extensionForMime(mime: String): String = when (mime) {
    "image/png" -> "png"
    "image/webp" -> "webp"
    else -> "jpg"
}

/** 一時ファイルが消してよいほど古いか。 */
internal fun isStaleTempFile(lastModifiedMs: Long, nowMs: Long): Boolean = nowMs - lastModifiedMs > TEMP_FILE_MAX_AGE_MS

/** 前回の cursor より後ろの行か（DATE_ADDED 秒, _ID の昇順）。 */
internal fun isAfterCursor(dateAdded: Long, id: Long, cursorDate: Long, cursorId: Long): Boolean =
    dateAdded > cursorDate || (dateAdded == cursorDate && id > cursorId)

/** 直近に自分で insert した uri を一定数・一定時間だけ覚える。スレッドセーフ。 */
internal class RecentUriSet(private val max: Int = RECENT_URI_MAX, private val ttlMs: Long = RECENT_URI_TTL_MS) {
    private val entries = LinkedHashMap<String, Long>()

    @Synchronized
    fun add(uri: String, nowMs: Long) {
        entries.remove(uri)
        entries[uri] = nowMs
        prune(nowMs)
    }

    @Synchronized
    fun contains(uri: String, nowMs: Long): Boolean {
        prune(nowMs)
        return entries.containsKey(uri)
    }

    private fun prune(nowMs: Long) {
        val iterator = entries.entries.iterator()
        while (iterator.hasNext()) {
            if (nowMs - iterator.next().value > ttlMs) iterator.remove() else break
        }
        while (entries.size > max) {
            val first = entries.keys.iterator()
            first.next()
            first.remove()
        }
    }
}
