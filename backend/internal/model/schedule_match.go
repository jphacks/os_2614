package model

import (
	"context"
	"database/sql"
	"encoding/binary"
	"math"

	"github.com/applemango/chain/internal/db"
)

// MatchEntry は照合（09 §8.4）に使う予定の 1 件。embedding は復号済みで、未計算なら nil。
type MatchEntry struct {
	ID        string
	DID       string
	Title     string
	Date      string
	StartMin  int
	EndMin    int
	Model     string
	Embedding []float32
}

const matchEntryColumns = `e.id, e.did, e.title, e.date, e.start_min, e.end_min, e.embed_model, e.embedding`

// ListNaturalMatchCandidates は date の予定のうち、natural_groups = 1 の人で、
// start_min - leadMin <= nowMin <= end_min かつ embedding が NULL でないものを返す（09 §8.4）。
func ListNaturalMatchCandidates(ctx context.Context, q db.Querier, date string, nowMin, leadMin int) ([]MatchEntry, error) {
	return db.GetItems(ctx, q, `
		SELECT `+matchEntryColumns+`
		FROM schedule_entries e
		JOIN user_settings s ON s.did = e.did
		WHERE e.date = ? AND e.embedding IS NOT NULL AND s.natural_groups = 1
		  AND e.start_min - ? <= ? AND ? <= e.end_min
		ORDER BY e.did, e.start_min, e.id`,
		[]any{date, leadMin, nowMin, nowMin}, scanMatchEntry)
}

// ListNaturalMatchEntriesForUsers は dids の date の予定のうち、embedding が NULL でないものを返す。
// 既存グループの代表（medoid）をメンバーの予定から求めるときに使う（09 §8.5）。
func ListNaturalMatchEntriesForUsers(ctx context.Context, q db.Querier, dids []string, date string) ([]MatchEntry, error) {
	if len(dids) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(dids)+1)
	args = append(args, date)
	for _, d := range dids {
		args = append(args, d)
	}
	return db.GetItems(ctx, q, `
		SELECT `+matchEntryColumns+`
		FROM schedule_entries e
		WHERE e.date = ? AND e.embedding IS NOT NULL AND e.did IN (`+placeholders(len(dids))+`)
		ORDER BY e.did, e.start_min, e.id`,
		args, scanMatchEntry)
}

func scanMatchEntry(scan db.Scan, e *MatchEntry) error {
	var model sql.NullString
	var blob []byte
	if err := scan(&e.ID, &e.DID, &e.Title, &e.Date, &e.StartMin, &e.EndMin, &model, &blob); err != nil {
		return err
	}
	if model.Valid {
		e.Model = model.String
	}
	e.Embedding = decodeMatchEmbedding(blob)
	return nil
}

// decodeMatchEmbedding は schedule_entries.embedding の BLOB を float32 の列に復号する。
// 保存は float32 リトルエンディアン（04 §2.5）。DecodeEmbedding とは別に、照合の読み出し専用に持つ。
func decodeMatchEmbedding(b []byte) []float32 {
	if len(b) < 4 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}
