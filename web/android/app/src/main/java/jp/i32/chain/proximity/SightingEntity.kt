package jp.i32.chain.proximity

import androidx.room.Entity
import androidx.room.PrimaryKey

/**
 * token ごとの sighting 集約を Room に永続化する行。
 * 純粋な [Aggregate] と同じフィールドを持ち、プロセス死・端末再起動をまたいで保持する。
 */
@Entity(tableName = "sightings")
data class SightingEntity(
    @PrimaryKey val token: String,
    val firstMs: Long,
    val lastMs: Long,
    val maxRssi: Int,
    val count: Int,
)

/** Room 行 → 純粋な集約型。 */
fun SightingEntity.toAggregate(): Aggregate = Aggregate(firstMs, lastMs, maxRssi, count)

/** 純粋な集約型 → Room 行。 */
fun Aggregate.toEntity(token: String): SightingEntity =
    SightingEntity(token, firstMs, lastMs, maxRssi, count)
