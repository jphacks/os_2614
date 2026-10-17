package model

import (
	"context"
	"database/sql"
	"encoding/binary"
	"math"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// 09 §8.2 の予定。今日と明日の分だけを持ち、昨日以前は janitor が消す（04 §4）。

// source の値。同じ source の予定だけをまとめて置き換える。
const (
	ScheduleSourceCalendar = "calendar"
	ScheduleSourceManual   = "manual"
)

// ScheduleEntry は schedule_entries の行（04 §2.4）。
type ScheduleEntry struct {
	ID         string
	DID        string
	Date       string // Asia/Tokyo の YYYY-MM-DD
	Source     string
	StartMin   int // 0:00 からの分
	EndMin     int
	Title      string
	Embedding  []float32 // 未計算なら nil
	EmbedModel string    // 未計算なら ""
	CreatedAt  time.Time
}

const scheduleEntryColumns = `id, did, date, source, start_min, end_min, title, embedding, embed_model, created_at`

func scanScheduleEntry(scan db.Scan, e *ScheduleEntry) error {
	var created int64
	var blob []byte
	var embedModel sql.NullString
	if err := scan(&e.ID, &e.DID, &e.Date, &e.Source, &e.StartMin, &e.EndMin, &e.Title,
		&blob, &embedModel, &created); err != nil {
		return err
	}
	e.Embedding = DecodeEmbedding(blob)
	if embedModel.Valid {
		e.EmbedModel = embedModel.String
	}
	e.CreatedAt = clock.FromUnixMilli(created)
	return nil
}

// ScheduleEntryInput は置き換えで保存する 1 件。ID は service が Deps.IDs で作る。
type ScheduleEntryInput struct {
	ID       string
	Title    string
	StartMin int
	EndMin   int
}

// ReplaceScheduleEntries は (did, date, source) の予定を全部消してから entries を入れ直す（09 §8.2）。
// 呼び出し側（service）が 1 つのトランザクションの中で呼ぶ。保存した行を source 付きで返す（embedding はまだ無い）。
func ReplaceScheduleEntries(ctx context.Context, q db.Querier, did, date, source string, entries []ScheduleEntryInput, now time.Time) ([]ScheduleEntry, error) {
	if _, err := db.Execute(ctx, q,
		"DELETE FROM schedule_entries WHERE did = ? AND date = ? AND source = ?",
		did, date, source); err != nil {
		return nil, err
	}
	out := make([]ScheduleEntry, 0, len(entries))
	ms := clock.UnixMilli(now)
	for _, e := range entries {
		if _, err := db.Execute(ctx, q, `
			INSERT INTO schedule_entries (id, did, date, source, start_min, end_min, title, embedding, embed_model, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?)`,
			e.ID, did, date, source, e.StartMin, e.EndMin, e.Title, ms); err != nil {
			return nil, err
		}
		out = append(out, ScheduleEntry{
			ID:        e.ID,
			DID:       did,
			Date:      date,
			Source:    source,
			StartMin:  e.StartMin,
			EndMin:    e.EndMin,
			Title:     e.Title,
			CreatedAt: now,
		})
	}
	return out, nil
}

// ListScheduleEntries は did の date（YYYY-MM-DD）の予定を開始時刻順に返す（09 §8.2）。
func ListScheduleEntries(ctx context.Context, q db.Querier, did, date string) ([]ScheduleEntry, error) {
	return db.GetItems(ctx, q, `
		SELECT `+scheduleEntryColumns+` FROM schedule_entries
		WHERE did = ? AND date = ?
		ORDER BY start_min, source, id`,
		[]any{did, date}, scanScheduleEntry)
}

// DeleteScheduleEntries は (did, date, source) の予定を消して件数を返す。
// source が空なら両方の source を消す（09 §8.2）。
func DeleteScheduleEntries(ctx context.Context, q db.Querier, did, date, source string) (int64, error) {
	if source == "" {
		return affected(db.Execute(ctx, q,
			"DELETE FROM schedule_entries WHERE did = ? AND date = ?", did, date))
	}
	return affected(db.Execute(ctx, q,
		"DELETE FROM schedule_entries WHERE did = ? AND date = ? AND source = ?", did, date, source))
}

// ListEntriesMissingEmbedding は embedding が NULL か、embed_model が現在の model と違う予定を
// 日付の古い順に最大 limit 件返す（09 §8.3 の再計算）。
func ListEntriesMissingEmbedding(ctx context.Context, q db.Querier, model string, limit int) ([]ScheduleEntry, error) {
	return db.GetItems(ctx, q, `
		SELECT `+scheduleEntryColumns+` FROM schedule_entries
		WHERE embedding IS NULL OR embed_model IS NULL OR embed_model <> ?
		ORDER BY date, start_min, id LIMIT ?`,
		[]any{model, limit}, scanScheduleEntry)
}

// SetEntryEmbedding は embedding と embed_model を保存する（09 §8.3）。
func SetEntryEmbedding(ctx context.Context, q db.Querier, id string, vec []float32, model string) error {
	_, err := db.Execute(ctx, q,
		"UPDATE schedule_entries SET embedding = ?, embed_model = ? WHERE id = ?",
		EncodeEmbedding(vec), model, id)
	return err
}

// DeleteScheduleEntriesBefore は date が before（YYYY-MM-DD）より前の予定を最大 limit 件消す（04 §4）。
func DeleteScheduleEntriesBefore(ctx context.Context, q db.Querier, before string, limit int) (int64, error) {
	return affected(db.Execute(ctx, q, `
		DELETE FROM schedule_entries WHERE id IN (
			SELECT id FROM schedule_entries WHERE date < ? ORDER BY date, start_min LIMIT ?)`,
		before, limit))
}

// EncodeEmbedding は float32 のベクトルをリトルエンディアンの BLOB にする（09 §8.3）。
// 照合側も DecodeEmbedding と対で使う。
func EncodeEmbedding(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// DecodeEmbedding は EncodeEmbedding の逆。空（NULL）か長さが 4 の倍数でなければ nil を返す。
func DecodeEmbedding(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}
