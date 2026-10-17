package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
)

// ScheduleService は今日・明日の予定（09 §8.2、14「自分」）。
type ScheduleService struct {
	Deps
	// Embedder は予定の embedding を計算する（09 §8.3）。nil なら計算しない（CHAIN_EMBED_URL が空）。
	Embedder Embedder
}

const (
	// ScheduleMaxEntries は 1 source・1 日あたりの予定の上限（09 §8.2、android §11.2）。
	ScheduleMaxEntries = 20
	// ScheduleMaxTitleRunes は予定のタイトルの文字数（09 §8.2）。
	ScheduleMaxTitleRunes = 100
)

// ScheduleInput は PUT /me/schedule-today で受け取る 1 件。title は平文のまま扱う（README L8）。
type ScheduleInput struct {
	Title string
	Start string
	End   string
}

// ScheduleDay は 1 日の予定。GET / PUT の応答になる。
type ScheduleDay struct {
	Date    string
	Entries []model.ScheduleEntry
}

// ReplaceToday は (did, date, source) の予定だけをまとめて置き換える（09 §8.2）。
// entries が空ならその source を消す。保存後、その日の全予定（両 source）を返す。
func (s *ScheduleService) ReplaceToday(ctx context.Context, did, date, source string, entries []ScheduleInput) (*ScheduleDay, error) {
	now := s.Clock.Now()
	if err := checkScheduleDate(date, now); err != nil {
		return nil, err
	}
	if err := checkScheduleSource(source, true); err != nil {
		return nil, err
	}
	parsed, err := s.parseScheduleEntries(entries)
	if err != nil {
		return nil, err
	}

	var saved []model.ScheduleEntry
	if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		var err error
		saved, err = model.ReplaceScheduleEntries(ctx, tx, did, date, source, parsed, now)
		return err
	}); err != nil {
		return nil, err
	}

	// embedding の計算は保存の後。失敗しても保存は成功させる（09 §8.3）。
	s.embed(ctx, saved)

	list, err := model.ListScheduleEntries(ctx, s.DB, did, date)
	if err != nil {
		return nil, err
	}
	return &ScheduleDay{Date: date, Entries: list}, nil
}

// List は did の date（YYYY-MM-DD）の予定を返す。date が空なら今日（09 §8.2）。
func (s *ScheduleService) List(ctx context.Context, did, date string) (*ScheduleDay, error) {
	now := s.Clock.Now()
	if date == "" {
		date = jstDateString(now)
	}
	if err := checkScheduleDate(date, now); err != nil {
		return nil, err
	}
	list, err := model.ListScheduleEntries(ctx, s.DB, did, date)
	if err != nil {
		return nil, err
	}
	return &ScheduleDay{Date: date, Entries: list}, nil
}

// Delete は (did, date, source) の予定を消す。date が空なら今日、source が空なら両方の source（09 §8.2）。
func (s *ScheduleService) Delete(ctx context.Context, did, date, source string) error {
	now := s.Clock.Now()
	if date == "" {
		date = jstDateString(now)
	}
	if err := checkScheduleDate(date, now); err != nil {
		return err
	}
	if err := checkScheduleSource(source, false); err != nil {
		return err
	}
	_, err := model.DeleteScheduleEntries(ctx, s.DB, did, date, source)
	return err
}

// BackfillEmbeddings は embedding の無い（または embed_model が古い）予定を最大 EmbedBackfillBatch 件
// 計算し直す（09 §8.3）。計算した件数を返す。Embedder が無ければ何もしない。
func (s *ScheduleService) BackfillEmbeddings(ctx context.Context) (int, error) {
	if s.Embedder == nil {
		return 0, nil
	}
	entries, err := model.ListEntriesMissingEmbedding(ctx, s.DB, s.Embedder.Model(), EmbedBackfillBatch)
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, nil
	}
	s.embed(ctx, entries)
	return len(entries), nil
}

// embed は予定のタイトルの embedding を計算して保存する。失敗しても呼び出し元の保存は失敗させない（09 §8.3）。
func (s *ScheduleService) embed(ctx context.Context, entries []model.ScheduleEntry) {
	if s.Embedder == nil || len(entries) == 0 {
		return
	}
	titles := make([]string, len(entries))
	for i, e := range entries {
		titles[i] = e.Title
	}
	embedCtx, cancel := context.WithTimeout(ctx, EmbedTimeout)
	defer cancel()
	vecs, err := s.Embedder.Embed(embedCtx, titles)
	if err != nil {
		// 予定のタイトルはログに出さない（02 §7）。
		slog.WarnContext(ctx, "embed failed; leaving schedule entries without embedding", "err", err, "count", len(entries))
		return
	}
	if len(vecs) != len(entries) {
		slog.WarnContext(ctx, "embed returned wrong count", "want", len(entries), "got", len(vecs))
		return
	}
	modelName := s.Embedder.Model()
	for i, v := range vecs {
		if err := model.SetEntryEmbedding(ctx, s.DB, entries[i].ID, v, modelName); err != nil {
			slog.WarnContext(ctx, "schedule embedding write failed", "err", err)
			return
		}
	}
}

// parseScheduleEntries は入力を検証して保存用の形にする。ID は検証を通ってから作る。
func (s *ScheduleService) parseScheduleEntries(entries []ScheduleInput) ([]model.ScheduleEntryInput, error) {
	fields := map[string]string{}
	if len(entries) > ScheduleMaxEntries {
		fields["entries"] = fmt.Sprintf("%d 件までです", ScheduleMaxEntries)
	}
	type parsed struct {
		start, end int
	}
	vals := make([]parsed, len(entries))
	for i, e := range entries {
		if n := utf8.RuneCountInString(e.Title); n == 0 || n > ScheduleMaxTitleRunes {
			fields[fmt.Sprintf("entries[%d].title", i)] = fmt.Sprintf("1〜%d 文字にしてください", ScheduleMaxTitleRunes)
		}
		start, okStart := parseScheduleClock(e.Start)
		end, okEnd := parseScheduleClock(e.End)
		if !okStart {
			fields[fmt.Sprintf("entries[%d].start", i)] = "HH:MM で指定してください"
		}
		if !okEnd {
			fields[fmt.Sprintf("entries[%d].end", i)] = "HH:MM で指定してください"
		}
		if okStart && okEnd && start >= end {
			fields[fmt.Sprintf("entries[%d].end", i)] = "start より後にしてください"
		}
		vals[i] = parsed{start: start, end: end}
	}
	if len(fields) > 0 {
		return nil, httpx.ErrInvalidRequest.With("fields", fields)
	}

	out := make([]model.ScheduleEntryInput, len(entries))
	for i, e := range entries {
		out[i] = model.ScheduleEntryInput{
			ID:       s.IDs.NewID(),
			Title:    e.Title,
			StartMin: vals[i].start,
			EndMin:   vals[i].end,
		}
	}
	return out, nil
}

// checkScheduleDate は date が今日か明日（Asia/Tokyo）であることを確かめる（09 §8.2）。
func checkScheduleDate(date string, now time.Time) error {
	today := jstDateString(now)
	tomorrow := jstDateString(now.Add(24 * time.Hour))
	if date != today && date != tomorrow {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"date": "今日か明日だけ指定できます"})
	}
	return nil
}

// checkScheduleSource は source を確かめる。required が false のときは空（両方）を許す。
func checkScheduleSource(source string, required bool) error {
	if source == "" {
		if required {
			return httpx.ErrInvalidRequest.With("fields", map[string]string{"source": "必須です"})
		}
		return nil
	}
	if source != model.ScheduleSourceCalendar && source != model.ScheduleSourceManual {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"source": "calendar か manual を指定してください"})
	}
	return nil
}

// jstDateString は t の Asia/Tokyo の暦日を YYYY-MM-DD で返す。
func jstDateString(t time.Time) string { return t.In(jst).Format("2006-01-02") }

// parseScheduleClock は "HH:MM" を 0:00 からの分にする。日をまたぐ予定の切り詰めで来る "24:00" だけを
// 終端として許す（android §11.2）。"25:00" や "24:30" は不正（09 §8.2）。
func parseScheduleClock(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' || !digits(s[0:2]) || !digits(s[3:5]) {
		return 0, false
	}
	hh, _ := strconv.Atoi(s[0:2])
	mm, _ := strconv.Atoi(s[3:5])
	if mm > 59 {
		return 0, false
	}
	if hh == 24 {
		if mm == 0 {
			return 24 * 60, true
		}
		return 0, false
	}
	if hh > 23 {
		return 0, false
	}
	return hh*60 + mm, true
}

func digits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
