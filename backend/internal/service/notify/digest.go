package notify

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/model"
	encountermodel "github.com/applemango/chain/internal/model/encounter"
	"github.com/applemango/chain/internal/service/core"
)

// 12 §5 のダイジェストの種類。
const (
	NotificationEncounterDaily   = "encounter.daily"
	NotificationEncounterWeekly  = "encounter.weekly"
	NotificationEncounterMonthly = "encounter.monthly"
)

// digestHour はダイジェストを積み始める時刻（JST、12 §5）。これより前は積まない。
const digestHour = 21

// DigestBatch は 1 回に処理するユーザー数（12 §5）。
const DigestBatch = 500

// Digest はすれ違いのダイジェスト通知を outbox に積む（12 §5）。
type Digest struct {
	core.Deps
}

type digestPeriod struct {
	typ    string
	period string
	from   time.Time
	to     time.Time
}

// Run は now の時点で積むべきダイジェストを積む（12 §5）。21:00 JST 以降に呼ぶ。
// 21:00 を逃しても、その日の 23:59 までに呼べばその日分を積む。
func (d *Digest) Run(ctx context.Context, now time.Time) error {
	local := now.In(core.Jst)
	if local.Hour() < digestHour {
		return nil
	}
	for _, p := range digestPeriods(now, local) {
		if err := d.runPeriod(ctx, now, p); err != nil {
			return err
		}
	}
	return nil
}

// digestPeriods は now に積むべき期間を返す。毎日 daily、月曜は前週の weekly、1 日は前月の monthly（12 §5）。
func digestPeriods(now time.Time, local time.Time) []digestPeriod {
	todayStart, _ := core.JstDayBounds(now)
	periods := []digestPeriod{{
		typ:    NotificationEncounterDaily,
		period: local.Format("2006-01-02"),
		from:   todayStart,
		to:     now.Add(-core.SummaryHoldback),
	}}
	if local.Weekday() == time.Monday {
		thisWeek := core.JstWeekStart(now)
		prevWeek := thisWeek.AddDate(0, 0, -7)
		periods = append(periods, digestPeriod{
			typ:    NotificationEncounterWeekly,
			period: prevWeek.Format("2006-01-02"),
			from:   prevWeek,
			to:     thisWeek,
		})
	}
	if local.Day() == 1 {
		thisMonth := core.JstMonthStart(now)
		prevMonth := thisMonth.AddDate(0, -1, 0)
		periods = append(periods, digestPeriod{
			typ:    NotificationEncounterMonthly,
			period: prevMonth.Format("2006-01"),
			from:   prevMonth,
			to:     thisMonth,
		})
	}
	return periods
}

func (d *Digest) runPeriod(ctx context.Context, now time.Time, p digestPeriod) error {
	after := ""
	for {
		dids, err := model.ListUserDIDs(ctx, d.DB, after, DigestBatch)
		if err != nil {
			return err
		}
		for _, did := range dids {
			if err := d.enqueue(ctx, now, did, p); err != nil {
				return err
			}
		}
		if len(dids) < DigestBatch {
			return nil
		}
		after = dids[len(dids)-1]
	}
}

// enqueue は did の回数を数え、1 件以上・通知が on・同じ期間のものがまだなければ積む（12 §5）。
func (d *Digest) enqueue(ctx context.Context, now time.Time, did string, p digestPeriod) error {
	n, err := encountermodel.CountEncountersForUserBetween(ctx, d.DB, did, p.from, p.to)
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	payload := digestPayload{Text: digestText(p, n), Period: p.period}
	return db.Transaction(ctx, d.DB, func(tx *sql.Tx) error {
		exists, err := model.HasDigestNotification(ctx, tx, did, p.typ, p.period)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
		_, err = EnqueueNotification(ctx, tx, d.Deps, did, p.typ, payload, now)
		return err
	})
}

// digestPayload はダイジェストの payload（12 §5）。period は重複判定に使う。
type digestPayload struct {
	Text   string `json:"text"`
	Period string `json:"period"`
}

// digestText は本文を組み立てる（12 §3）。相手の名前は入れない。
func digestText(p digestPeriod, n int) string {
	switch p.typ {
	case NotificationEncounterDaily:
		return fmt.Sprintf("今日のすれ違い: %d 人", n)
	case NotificationEncounterWeekly:
		return fmt.Sprintf("先週のすれ違い: %d 人", n)
	case NotificationEncounterMonthly:
		if t, err := time.Parse("2006-01", p.period); err == nil {
			return fmt.Sprintf("%d 月のすれ違い: %d 人", int(t.Month()), n)
		}
		return fmt.Sprintf("すれ違い: %d 人", n)
	default:
		return fmt.Sprintf("すれ違い: %d 人", n)
	}
}
