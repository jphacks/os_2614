//go:build integration

package natural_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/applemango/chain/internal/httpx"
	schedulemodel "github.com/applemango/chain/internal/model/schedule"
	naturalsvc "github.com/applemango/chain/internal/service/natural"
	"github.com/applemango/chain/internal/testutil"
	"github.com/applemango/chain/internal/worker"
)

// 09 §8.2 / §8.3 の予定の保存・置き換え・embedding。

const (
	schedAlice     = "did:plc:sched-alice"
	schedToday     = "2026-10-17" // testutil.Epoch の Asia/Tokyo の暦日
	schedTomorrow  = "2026-10-18"
	schedYesterday = "2026-10-16"
)

type fakeEmbedder struct {
	model string
	fn    func(texts []string) ([][]float32, error)
}

func (f *fakeEmbedder) Model() string { return f.model }

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if f.fn == nil {
		return nil, errors.New("fakeEmbedder: no fn")
	}
	return f.fn(texts)
}

func vectorFor(v []float32) func([]string) ([][]float32, error) {
	return func(texts []string) ([][]float32, error) {
		out := make([][]float32, len(texts))
		for i := range texts {
			out[i] = v
		}
		return out, nil
	}
}

func listSchedule(t *testing.T, env *testutil.Env, did, date string) []schedulemodel.ScheduleEntry {
	t.Helper()
	entries, err := schedulemodel.ListScheduleEntries(context.Background(), env.DB, did, date)
	if err != nil {
		t.Fatalf("ListScheduleEntries(%s): %v", date, err)
	}
	return entries
}

func TestScheduleReplaceIsolatesSources(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, schedAlice, "alice.test")
	svc := env.Schedule()

	if _, err := svc.ReplaceToday(ctx, schedAlice, schedToday, "calendar",
		[]naturalsvc.ScheduleInput{{Title: "線形代数 II", Start: "10:40", End: "12:10"}}); err != nil {
		t.Fatalf("ReplaceToday(calendar): %v", err)
	}
	day, err := svc.ReplaceToday(ctx, schedAlice, schedToday, "manual",
		[]naturalsvc.ScheduleInput{{Title: "ランチ", Start: "12:30", End: "13:00"}})
	if err != nil {
		t.Fatalf("ReplaceToday(manual): %v", err)
	}
	if len(day.Entries) != 2 {
		t.Fatalf("entries = %d, want 2 (calendar + manual)", len(day.Entries))
	}

	// calendar を置き換えても manual は残る（09 §8.2）。
	if _, err := svc.ReplaceToday(ctx, schedAlice, schedToday, "calendar",
		[]naturalsvc.ScheduleInput{{Title: "線形代数学 II", Start: "13:00", End: "14:30"}}); err != nil {
		t.Fatalf("ReplaceToday(calendar, again): %v", err)
	}
	entries := listSchedule(t, env, schedAlice, schedToday)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	var calendarTitles, manualTitles []string
	for _, e := range entries {
		switch e.Source {
		case "calendar":
			calendarTitles = append(calendarTitles, e.Title)
		case "manual":
			manualTitles = append(manualTitles, e.Title)
		}
	}
	if !reflect.DeepEqual(calendarTitles, []string{"線形代数学 II"}) {
		t.Fatalf("calendar titles = %v, want [線形代数学 II]", calendarTitles)
	}
	if !reflect.DeepEqual(manualTitles, []string{"ランチ"}) {
		t.Fatalf("manual titles = %v, want [ランチ]", manualTitles)
	}
}

func TestScheduleEmptyEntriesDeletesSource(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, schedAlice, "alice.test")
	svc := env.Schedule()

	if _, err := svc.ReplaceToday(ctx, schedAlice, schedToday, "calendar",
		[]naturalsvc.ScheduleInput{{Title: "予定", Start: "09:00", End: "10:00"}}); err != nil {
		t.Fatalf("calendar: %v", err)
	}
	if _, err := svc.ReplaceToday(ctx, schedAlice, schedToday, "manual",
		[]naturalsvc.ScheduleInput{{Title: "手入力", Start: "11:00", End: "12:00"}}); err != nil {
		t.Fatalf("manual: %v", err)
	}

	day, err := svc.ReplaceToday(ctx, schedAlice, schedToday, "calendar", nil)
	if err != nil {
		t.Fatalf("clear calendar: %v", err)
	}
	if len(day.Entries) != 1 || day.Entries[0].Source != "manual" {
		t.Fatalf("entries after clearing calendar = %+v, want only manual", day.Entries)
	}
}

func TestScheduleListAndDelete(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, schedAlice, "alice.test")
	svc := env.Schedule()

	if _, err := svc.ReplaceToday(ctx, schedAlice, schedToday, "calendar",
		[]naturalsvc.ScheduleInput{{Title: "A", Start: "09:00", End: "10:00"}}); err != nil {
		t.Fatalf("calendar: %v", err)
	}
	if _, err := svc.ReplaceToday(ctx, schedAlice, schedTomorrow, "manual",
		[]naturalsvc.ScheduleInput{{Title: "B", Start: "09:00", End: "10:00"}}); err != nil {
		t.Fatalf("manual: %v", err)
	}

	// date を省くと今日。
	day, err := svc.List(ctx, schedAlice, "")
	if err != nil {
		t.Fatalf("List(today): %v", err)
	}
	if day.Date != schedToday || len(day.Entries) != 1 {
		t.Fatalf("List(today) = %+v, want date=%s entries=1", day, schedToday)
	}
	if day, err = svc.List(ctx, schedAlice, schedTomorrow); err != nil || len(day.Entries) != 1 {
		t.Fatalf("List(tomorrow) = %+v, err=%v, want 1 entry", day, err)
	}

	// source を指定するとその source だけ消える。
	if err := svc.Delete(ctx, schedAlice, schedToday, "calendar"); err != nil {
		t.Fatalf("Delete(calendar): %v", err)
	}
	if entries := listSchedule(t, env, schedAlice, schedToday); len(entries) != 0 {
		t.Fatalf("calendar entries after delete = %d, want 0", len(entries))
	}

	// source を省くと両方。
	if err := svc.Delete(ctx, schedAlice, schedTomorrow, ""); err != nil {
		t.Fatalf("Delete(both): %v", err)
	}
	if entries := listSchedule(t, env, schedAlice, schedTomorrow); len(entries) != 0 {
		t.Fatalf("entries after delete both = %d, want 0", len(entries))
	}
}

func TestScheduleValidation(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateUser(t, schedAlice, "alice.test")
	svc := env.Schedule()
	ctx := context.Background()

	many := make([]naturalsvc.ScheduleInput, 21)
	for i := range many {
		many[i] = naturalsvc.ScheduleInput{Title: "x", Start: "09:00", End: "10:00"}
	}
	longTitle := ""
	for i := 0; i < 101; i++ {
		longTitle += "あ"
	}

	cases := []struct {
		name    string
		date    string
		source  string
		entries []naturalsvc.ScheduleInput
		field   string
	}{
		{"yesterday", schedYesterday, "calendar", nil, "date"},
		{"day after tomorrow", "2026-10-19", "calendar", nil, "date"},
		{"bad source", schedToday, "work", nil, "source"},
		{"too many entries", schedToday, "calendar", many, "entries"},
		{"long title", schedToday, "calendar", []naturalsvc.ScheduleInput{{Title: longTitle, Start: "09:00", End: "10:00"}}, "entries[0].title"},
		{"empty title", schedToday, "calendar", []naturalsvc.ScheduleInput{{Title: "", Start: "09:00", End: "10:00"}}, "entries[0].title"},
		{"start equals end", schedToday, "calendar", []naturalsvc.ScheduleInput{{Title: "x", Start: "10:00", End: "10:00"}}, "entries[0].end"},
		{"start after end", schedToday, "calendar", []naturalsvc.ScheduleInput{{Title: "x", Start: "11:00", End: "10:00"}}, "entries[0].end"},
		{"hour 25", schedToday, "calendar", []naturalsvc.ScheduleInput{{Title: "x", Start: "25:00", End: "26:00"}}, "entries[0].start"},
		{"minute 61", schedToday, "calendar", []naturalsvc.ScheduleInput{{Title: "x", Start: "09:61", End: "10:00"}}, "entries[0].start"},
		{"end 24:30", schedToday, "calendar", []naturalsvc.ScheduleInput{{Title: "x", Start: "09:00", End: "24:30"}}, "entries[0].end"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.ReplaceToday(ctx, schedAlice, tc.date, tc.source, tc.entries)
			var e *httpx.Error
			if !errors.As(err, &e) {
				t.Fatalf("err = %v, want *httpx.Error", err)
			}
			if e.Code != "invalid_request" {
				t.Fatalf("code = %q, want invalid_request", e.Code)
			}
			fields, _ := e.Details["fields"].(map[string]string)
			if fields[tc.field] == "" {
				t.Fatalf("fields[%q] is empty (fields=%v)", tc.field, fields)
			}
		})
	}

	// "24:00" は日をまたぐ予定の切り詰めで来るので許す（android §11.2）。
	if _, err := svc.ReplaceToday(ctx, schedAlice, schedToday, "calendar",
		[]naturalsvc.ScheduleInput{{Title: "終日会議", Start: "09:00", End: "24:00"}}); err != nil {
		t.Fatalf("end 24:00 should be accepted: %v", err)
	}
}

func TestScheduleEmbeddingOnSave(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, schedAlice, "alice.test")
	env.Embedder = &fakeEmbedder{model: "model-a", fn: vectorFor([]float32{1, 0, 0})}

	svc := env.Schedule()
	if _, err := svc.ReplaceToday(ctx, schedAlice, schedToday, "calendar",
		[]naturalsvc.ScheduleInput{{Title: "線形代数 II", Start: "10:40", End: "12:10"}}); err != nil {
		t.Fatalf("ReplaceToday: %v", err)
	}

	entries := listSchedule(t, env, schedAlice, schedToday)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if !reflect.DeepEqual(entries[0].Embedding, []float32{1, 0, 0}) {
		t.Fatalf("embedding = %v, want [1 0 0]", entries[0].Embedding)
	}
	if entries[0].EmbedModel != "model-a" {
		t.Fatalf("embed_model = %q, want model-a", entries[0].EmbedModel)
	}
}

func TestScheduleEmbedFailureThenBackfill(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, schedAlice, "alice.test")

	// embedding が失敗しても保存は成功する（09 §8.3）。
	env.Embedder = &fakeEmbedder{model: "model-a", fn: func([]string) ([][]float32, error) {
		return nil, errors.New("inference down")
	}}
	svc := env.Schedule()
	if _, err := svc.ReplaceToday(ctx, schedAlice, schedToday, "calendar",
		[]naturalsvc.ScheduleInput{{Title: "線形代数 II", Start: "10:40", End: "12:10"}}); err != nil {
		t.Fatalf("ReplaceToday should succeed even if embed fails: %v", err)
	}
	entries := listSchedule(t, env, schedAlice, schedToday)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Embedding != nil || entries[0].EmbedModel != "" {
		t.Fatalf("embedding = %v / model = %q, want NULL until backfill", entries[0].Embedding, entries[0].EmbedModel)
	}

	// ワーカーが後で計算し直す。
	env.Embedder = &fakeEmbedder{model: "model-a", fn: vectorFor([]float32{0, 1, 0})}
	backfill := &worker.EmbedBackfill{Deps: env.Deps, Schedule: env.Schedule()}
	if err := backfill.Tick(ctx); err != nil {
		t.Fatalf("EmbedBackfill.Tick: %v", err)
	}

	entries = listSchedule(t, env, schedAlice, schedToday)
	if !reflect.DeepEqual(entries[0].Embedding, []float32{0, 1, 0}) || entries[0].EmbedModel != "model-a" {
		t.Fatalf("after backfill: embedding = %v / model = %q", entries[0].Embedding, entries[0].EmbedModel)
	}
}

func TestScheduleBackfillRecomputesOnModelChange(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, schedAlice, "alice.test")

	env.Embedder = &fakeEmbedder{model: "model-a", fn: vectorFor([]float32{1, 0, 0})}
	if _, err := env.Schedule().ReplaceToday(ctx, schedAlice, schedToday, "calendar",
		[]naturalsvc.ScheduleInput{{Title: "線形代数 II", Start: "10:40", End: "12:10"}}); err != nil {
		t.Fatalf("ReplaceToday: %v", err)
	}

	// モデルが変わったら、同じ内容でも計算し直す（09 §8.3）。
	env.Embedder = &fakeEmbedder{model: "model-b", fn: vectorFor([]float32{0, 0, 1})}
	svc := env.Schedule()
	n, err := svc.BackfillEmbeddings(ctx)
	if err != nil {
		t.Fatalf("BackfillEmbeddings: %v", err)
	}
	if n != 1 {
		t.Fatalf("backfilled = %d, want 1", n)
	}
	entries := listSchedule(t, env, schedAlice, schedToday)
	if entries[0].EmbedModel != "model-b" || !reflect.DeepEqual(entries[0].Embedding, []float32{0, 0, 1}) {
		t.Fatalf("after model change: model = %q embedding = %v", entries[0].EmbedModel, entries[0].Embedding)
	}

	// もう計算済みなので 2 回目は 0 件。
	if n, err = svc.BackfillEmbeddings(ctx); err != nil || n != 0 {
		t.Fatalf("second backfill = %d, err = %v, want 0", n, err)
	}
}

func TestJanitorDeletesPastScheduleEntries(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	env.CreateUser(t, schedAlice, "alice.test")
	svc := env.Schedule()

	if _, err := svc.ReplaceToday(ctx, schedAlice, schedToday, "calendar",
		[]naturalsvc.ScheduleInput{{Title: "今日", Start: "09:00", End: "10:00"}}); err != nil {
		t.Fatalf("today: %v", err)
	}
	if _, err := svc.ReplaceToday(ctx, schedAlice, schedTomorrow, "calendar",
		[]naturalsvc.ScheduleInput{{Title: "明日", Start: "09:00", End: "10:00"}}); err != nil {
		t.Fatalf("tomorrow: %v", err)
	}
	// 昨日の予定は API からは作れないので、直接入れる。
	if _, err := env.DB.ExecContext(ctx, `
		INSERT INTO schedule_entries (id, did, date, source, start_min, end_min, title, created_at)
		VALUES ('sched-old', ?, ?, 'calendar', 540, 600, '昨日', 0)`,
		schedAlice, schedYesterday); err != nil {
		t.Fatalf("insert yesterday: %v", err)
	}

	res, err := env.Janitor().Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if res.ScheduleEntries != 1 {
		t.Fatalf("ScheduleEntries = %d, want 1", res.ScheduleEntries)
	}
	if n := len(listSchedule(t, env, schedAlice, schedYesterday)); n != 0 {
		t.Fatalf("yesterday entries = %d, want 0", n)
	}
	if n := len(listSchedule(t, env, schedAlice, schedToday)); n != 1 {
		t.Fatalf("today entries = %d, want 1", n)
	}
	if n := len(listSchedule(t, env, schedAlice, schedTomorrow)); n != 1 {
		t.Fatalf("tomorrow entries = %d, want 1", n)
	}
}
