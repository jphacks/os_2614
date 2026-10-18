package space

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// Record は records の行（04 §2.4）。instance の投稿・メディア・Pin をすべてここに置く。
type Record struct {
	URI        string
	SpaceID    string
	AuthorDID  string
	Collection string
	RKey       string
	ValueJSON  string
	ReplyRoot  string
	CreatedAt  time.Time
	DeletedAt  *time.Time
	DeletedBy  string
	ModLabel   string
	ModState   string
}

const RecordColumns = `uri, space_id, author_did, collection, rkey, value_json, reply_root, created_at, deleted_at, deleted_by, mod_label, mod_state`

func ScanRecord(scan db.Scan, r *Record) error {
	var created int64
	var deleted sql.NullInt64
	var deletedBy sql.NullString
	var replyRoot sql.NullString
	if err := scan(&r.URI, &r.SpaceID, &r.AuthorDID, &r.Collection, &r.RKey, &r.ValueJSON, &replyRoot,
		&created, &deleted, &deletedBy, &r.ModLabel, &r.ModState); err != nil {
		return err
	}
	r.CreatedAt = clock.FromUnixMilli(created)
	if deleted.Valid {
		t := clock.FromUnixMilli(deleted.Int64)
		r.DeletedAt = &t
	}
	r.DeletedBy = deletedBy.String
	r.ReplyRoot = replyRoot.String
	return nil
}

// ListReplyCounts は 10 §4.2 の返信数を、指定した root 群について 1 回で集計する。
func ListReplyCounts(ctx context.Context, q db.Querier, spaceID, viewer string, roots []string) (map[string]int, error) {
	counts := make(map[string]int, len(roots))
	if len(roots) == 0 {
		return counts, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(roots)), ",")
	args := []any{spaceID}
	for _, root := range roots {
		args = append(args, root)
	}
	args = append(args, viewer, viewer)
	rows, err := q.QueryContext(ctx, `SELECT reply_root, count(*) FROM records
		WHERE space_id = ? AND collection = 'jp.i32.chain.instance.post' AND reply_root IN (`+ph+`)
		AND deleted_at IS NULL AND mod_label <> 'block'
		AND NOT EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker = ? AND b.blocked = author_did) OR (b.blocker = author_did AND b.blocked = ?))
		GROUP BY reply_root`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var root string
		var n int
		if err := rows.Scan(&root, &n); err != nil {
			return nil, err
		}
		counts[root] = n
	}
	return counts, rows.Err()
}

// ListReplies は 10 §4.2.1 の root に属する返信を古い順に取得する。
func ListReplies(ctx context.Context, q db.Querier, spaceID, root string, afterAt int64, afterURI string, limit int) ([]Record, error) {
	query := "SELECT " + RecordColumns + " FROM records WHERE space_id = ? AND collection = 'jp.i32.chain.instance.post' AND reply_root = ? AND deleted_at IS NULL"
	args := []any{spaceID, root}
	if afterAt > 0 {
		query += " AND (created_at > ? OR (created_at = ? AND uri > ?))"
		args = append(args, afterAt, afterAt, afterURI)
	}
	query += " ORDER BY created_at ASC, uri ASC LIMIT ?"
	args = append(args, limit)
	return db.GetItems(ctx, q, query, args, ScanRecord)
}

func GetRecord(ctx context.Context, q db.Querier, uri string) (*Record, error) {
	return db.GetItem(ctx, q, "SELECT "+RecordColumns+" FROM records WHERE uri = ?", []any{uri}, ScanRecord)
}

func InsertRecord(ctx context.Context, q db.Querier, r Record) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO records (uri, space_id, author_did, collection, rkey, value_json, created_at, mod_label, mod_state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.URI, r.SpaceID, r.AuthorDID, r.Collection, r.RKey, r.ValueJSON,
		clock.UnixMilli(r.CreatedAt), r.ModLabel, r.ModState)
	return err
}

// SetRecordModeration は moderation の結果を後から付ける（11 §3）。
func SetRecordModeration(ctx context.Context, q db.Querier, uri, label, state string) error {
	_, err := db.Execute(ctx, q,
		"UPDATE records SET mod_label = ?, mod_state = ? WHERE uri = ?", label, state, uri)
	return err
}

// SoftDeleteRecord は論理削除する。既に削除済みなら false。
func SoftDeleteRecord(ctx context.Context, q db.Querier, uri, by string, now time.Time) (bool, error) {
	res, err := db.Execute(ctx, q,
		"UPDATE records SET deleted_at = ?, deleted_by = ? WHERE uri = ? AND deleted_at IS NULL",
		clock.UnixMilli(now), by, uri)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ListRecordsInSpace は指定した collection の、削除されていない record を新しい順で返す。
// before が非ゼロなら、その (created_at, uri) より前のものを返す（カーソル方式）。
func ListRecordsInSpace(ctx context.Context, q db.Querier, spaceID string, collections []string, beforeAt int64, beforeURI string, limit int) ([]Record, error) {
	return listRecordsInSpace(ctx, q, spaceID, collections, beforeAt, beforeURI, limit, false)
}

func ListRecordsInSpaceRoots(ctx context.Context, q db.Querier, spaceID string, collections []string, beforeAt int64, beforeURI string, limit int) ([]Record, error) {
	return listRecordsInSpace(ctx, q, spaceID, collections, beforeAt, beforeURI, limit, true)
}

func listRecordsInSpace(ctx context.Context, q db.Querier, spaceID string, collections []string, beforeAt int64, beforeURI string, limit int, rootsOnly bool) ([]Record, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(collections)), ",")
	args := []any{spaceID}
	for _, c := range collections {
		args = append(args, c)
	}
	query := "SELECT " + RecordColumns + " FROM records WHERE space_id = ? AND deleted_at IS NULL AND collection IN (" + placeholders + ")"
	if rootsOnly {
		query += " AND (collection <> 'jp.i32.chain.instance.post' OR reply_root IS NULL)"
	}
	if beforeAt > 0 {
		query += " AND (created_at < ? OR (created_at = ? AND uri < ?))"
		args = append(args, beforeAt, beforeAt, beforeURI)
	}
	query += " ORDER BY created_at DESC, uri DESC LIMIT ?"
	args = append(args, limit)
	return db.GetItems(ctx, q, query, args, ScanRecord)
}
