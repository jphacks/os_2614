package jp.i32.chain.proximity

import android.content.Context
import androidx.room.Dao
import androidx.room.Database
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.Room
import androidx.room.RoomDatabase

/**
 * sightings の DAO。呼び出し側 ([SightingStore]) が必ず IO ディスパッチャ上で実行するため、
 * すべて suspend にして main thread ガードに触れないようにする。
 */
@Dao
interface SightingDao {
    @Query("SELECT * FROM sightings")
    suspend fun getAll(): List<SightingEntity>

    @Query("SELECT * FROM sightings WHERE token = :token LIMIT 1")
    suspend fun find(token: String): SightingEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: SightingEntity)

    @Query("DELETE FROM sightings")
    suspend fun deleteAll()

    /** 24h ルール: lastMs <= cutoff の行は送らずに捨てる。 */
    @Query("DELETE FROM sightings WHERE lastMs <= :cutoff")
    suspend fun deleteExpired(cutoff: Long)

    @Query("SELECT COUNT(*) FROM sightings")
    suspend fun count(): Int
}

@Database(entities = [SightingEntity::class], version = 1, exportSchema = true)
abstract class SightingDatabase : RoomDatabase() {
    abstract fun sightingDao(): SightingDao

    companion object {
        const val NAME = "chain_sightings"

        @Volatile
        private var instance: SightingDatabase? = null

        /** アプリ全体で 1 つの DB 実例。テストは自前の実例を注入できる。 */
        fun get(context: Context): SightingDatabase =
            instance ?: synchronized(this) {
                instance ?: Room.databaseBuilder(
                    context.applicationContext,
                    SightingDatabase::class.java,
                    NAME,
                ).fallbackToDestructiveMigration(dropAllTables = true).build().also { instance = it }
            }
    }
}
