package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/model"
	spacemodel "github.com/applemango/chain/internal/model/space"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/moderation"
	"github.com/applemango/chain/internal/service/notify"
)

// ModerationQueueService は modq（11 §3）を消費し、pending の投稿を再判定する。
type ModerationQueueService struct {
	core.Deps
	Assessor moderation.PostAssessor
	Broker   realtime.Broker
	Pins     *PinService
}

const (
	// ModerationMaxAttempts は 1 件を諦めるまでの失敗回数（11 §3）。
	ModerationMaxAttempts  = 5
	moderationRequeueBatch = 100
	moderationBackoffBase  = 10 * time.Second
)

// ProcessPending は due な要素を再判定する。戻り値は modq から外した件数。
func (s *ModerationQueueService) ProcessPending(ctx context.Context) (int, error) {
	now := s.Clock.Now()
	entries, err := model.ListModerationJobs(ctx, s.Redis, moderationRequeueBatch)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		if e.Job.NextAt > 0 && now.UnixMilli() < e.Job.NextAt {
			continue
		}
		gone, err := s.processOne(ctx, e, now)
		if err != nil {
			return removed, err
		}
		if gone {
			removed++
		}
	}
	return removed, nil
}

// processOne は 1 件を再判定する。modq から外したら true。
func (s *ModerationQueueService) processOne(ctx context.Context, e model.ModerationJobEntry, now time.Time) (bool, error) {
	if e.Job.URI == "" {
		return true, model.RemoveModerationJob(ctx, s.Redis, e.Raw)
	}
	if isPublicPostURI(e.Job.URI) {
		return s.processPublicPost(ctx, e, now)
	}
	rec, err := spacemodel.GetRecord(ctx, s.DB, e.Job.URI)
	if errors.Is(err, sql.ErrNoRows) {
		return true, model.RemoveModerationJob(ctx, s.Redis, e.Raw)
	}
	if err != nil {
		return false, err
	}
	if rec.DeletedAt != nil || rec.ModState != "pending" {
		return true, model.RemoveModerationJob(ctx, s.Redis, e.Raw)
	}

	text := recordText(rec)
	if text == "" {
		if err := spacemodel.SetRecordModeration(ctx, s.DB, rec.URI, string(rec.ModLabel), "done"); err != nil {
			return false, err
		}
		return true, model.RemoveModerationJob(ctx, s.Redis, e.Raw)
	}

	assessCtx, cancel := context.WithTimeout(ctx, moderation.ModerationRequeueTimeout)
	verdict, pin, err := s.Assessor.Assess(assessCtx, moderation.Content{Kind: moderation.ContentKindInstancePost, Text: text})
	cancel()
	if err != nil {
		return s.requeue(ctx, e, now, err)
	}

	if verdict.Label == moderation.LabelBlock {
		if err := s.hide(ctx, rec, now); err != nil {
			return false, err
		}
		return true, model.RemoveModerationJob(ctx, s.Redis, e.Raw)
	}
	if err := spacemodel.SetRecordModeration(ctx, s.DB, rec.URI, string(verdict.Label), "done"); err != nil {
		return false, err
	}
	if pin && s.Pins != nil {
		if err := s.Pins.CreateAuto(ctx, rec.SpaceID, rec.URI); err != nil {
			moderation.LogModerationError(ctx, err)
		}
	}
	return true, model.RemoveModerationJob(ctx, s.Redis, e.Raw)
}

// processPublicPost は Jetstream から取り込んだ公開投稿を再判定する（11 §2、§3）。pin の対象ではない。
func (s *ModerationQueueService) processPublicPost(ctx context.Context, e model.ModerationJobEntry, now time.Time) (bool, error) {
	post, err := model.GetPublicPost(ctx, s.DB, e.Job.URI)
	if errors.Is(err, sql.ErrNoRows) {
		return true, model.RemoveModerationJob(ctx, s.Redis, e.Raw)
	}
	if err != nil {
		return false, err
	}
	if post.DeletedAt != nil || post.ModState != "pending" {
		return true, model.RemoveModerationJob(ctx, s.Redis, e.Raw)
	}
	if post.Text == "" {
		if err := model.SetPublicPostModeration(ctx, s.DB, post.URI, post.ModLabel, "done"); err != nil {
			return false, err
		}
		return true, model.RemoveModerationJob(ctx, s.Redis, e.Raw)
	}

	assessCtx, cancel := context.WithTimeout(ctx, moderation.ModerationRequeueTimeout)
	verdict, _, err := s.Assessor.Assess(assessCtx, moderation.Content{Kind: ContentKindPublicPost, Text: post.Text})
	cancel()
	if err != nil {
		return s.requeue(ctx, e, now, err)
	}
	if verdict.Label == moderation.LabelBlock {
		if err := s.hidePublicPost(ctx, post, now); err != nil {
			return false, err
		}
		return true, model.RemoveModerationJob(ctx, s.Redis, e.Raw)
	}
	if err := model.SetPublicPostModeration(ctx, s.DB, post.URI, string(verdict.Label), "done"); err != nil {
		return false, err
	}
	return true, model.RemoveModerationJob(ctx, s.Redis, e.Raw)
}

// requeue は判定の失敗を指数バックオフで modq に戻す。上限に達したら諦めて外す（11 §3）。
// 戻り値は modq から外したか。
func (s *ModerationQueueService) requeue(ctx context.Context, e model.ModerationJobEntry, now time.Time, cause error) (bool, error) {
	moderation.LogModerationError(ctx, cause)
	attempts := e.Job.Attempts + 1
	if attempts >= ModerationMaxAttempts {
		return true, model.RemoveModerationJob(ctx, s.Redis, e.Raw)
	}
	next := model.ModerationJob{URI: e.Job.URI, Attempts: attempts, NextAt: now.Add(moderationBackoff(attempts)).UnixMilli()}
	return false, model.RequeueModerationJob(ctx, s.Redis, e.Raw, next)
}

// isPublicPostURI は modq の URI が公開投稿のものか（at://{did}/jp.i32.chain.feed.post/{rkey}、03 §5）。
func isPublicPostURI(uri string) bool {
	return strings.HasPrefix(uri, "at://") && strings.Contains(uri, "/"+CollectionFeedPost+"/")
}

// hide は record を block（非表示）にし、投稿者に moderation.hidden を積む（11 §3、12 §3）。
// 通知は mod_label の更新と同じトランザクションで積む（12 §4）。
func (s *ModerationQueueService) hide(ctx context.Context, rec *spacemodel.Record, now time.Time) error {
	var item *notify.NotificationItem
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := spacemodel.SetRecordModeration(ctx, tx, rec.URI, string(moderation.LabelBlock), "done"); err != nil {
			return err
		}
		i, err := notify.EnqueueNotification(ctx, tx, s.Deps, rec.AuthorDID, notify.NotificationModerationHidden, notify.ModerationHiddenPayload{
			Text: "投稿が非表示になりました", SpaceID: rec.SpaceID, URI: rec.URI,
		}, now)
		if err != nil {
			return err
		}
		item = i
		return nil
	})
	if err != nil {
		return err
	}
	if item != nil && s.Broker != nil {
		s.Broker.Publish(ctx, realtime.Event{Name: "notification", User: rec.AuthorDID, Data: *item})
	}
	return nil
}

// hidePublicPost は取り込んだ公開投稿を block（非表示）にし、投稿者に moderation.hidden を積む（11 §3）。
func (s *ModerationQueueService) hidePublicPost(ctx context.Context, post *model.PublicPost, now time.Time) error {
	var item *notify.NotificationItem
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := model.SetPublicPostModeration(ctx, tx, post.URI, string(moderation.LabelBlock), "done"); err != nil {
			return err
		}
		i, err := notify.EnqueueNotification(ctx, tx, s.Deps, post.AuthorDID, notify.NotificationModerationHidden, notify.ModerationHiddenPayload{
			Text: "投稿が非表示になりました", URI: post.URI,
		}, now)
		if err != nil {
			return err
		}
		item = i
		return nil
	})
	if err != nil {
		return err
	}
	if item != nil && s.Broker != nil {
		s.Broker.Publish(ctx, realtime.Event{Name: "notification", User: post.AuthorDID, Data: *item})
	}
	return nil
}

// moderationBackoff は失敗回数に対する次の待ち時間を返す。1 回目 10 秒から倍々にする（11 §3）。
func moderationBackoff(attempts int) time.Duration {
	d := moderationBackoffBase
	for i := 1; i < attempts; i++ {
		d *= 2
	}
	return d
}

func recordText(rec *spacemodel.Record) string {
	var v PostValue
	if err := json.Unmarshal([]byte(rec.ValueJSON), &v); err != nil {
		return ""
	}
	return v.Text
}
