package service

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/storage"
)

// Janitor は期限切れデータの掃除（04 §4、13 §5 の 6）。
type Janitor struct {
	Deps
	Storage *storage.Store
}

// JanitorBatch は 1 回の実行で消す件数の上限。長時間のロックを避けるため、少しずつ消す。
const JanitorBatch = 1000

// JanitorResult は 1 回の掃除で消した件数。
type JanitorResult struct {
	BeaconTokens    int
	FriendInvites   int
	AppSessions     int
	UnattachedMedia int
	Records         int
	DeletedMedia    int
	Notifications   int
	ScheduleEntries int
	InvitesExpired  int
	InvitesDeleted  int
	GeoUsers        int
	Users           int
}

// Total は消した合計件数。
func (r JanitorResult) Total() int {
	return r.BeaconTokens + r.FriendInvites + r.AppSessions + r.UnattachedMedia +
		r.Records + r.DeletedMedia + r.Notifications + r.ScheduleEntries +
		r.InvitesExpired + r.InvitesDeleted + r.GeoUsers + r.Users
}

// Sweep は 04 §4 の表の行を 1 回だけ掃除する。各対象は JanitorBatch 件まで消す。
func (s *Janitor) Sweep(ctx context.Context) (JanitorResult, error) {
	now := s.Clock.Now()
	var res JanitorResult

	n, err := model.DeleteOldBeaconTokens(ctx, s.DB, now.Add(-48*time.Hour).Unix(), JanitorBatch)
	if err != nil {
		return res, err
	}
	res.BeaconTokens = int(n)

	n, err = model.DeleteExpiredFriendInvites(ctx, s.DB, now.Add(-24*time.Hour), JanitorBatch)
	if err != nil {
		return res, err
	}
	res.FriendInvites = int(n)

	n, err = model.DeleteOldAppSessions(ctx, s.DB, now.Add(-7*24*time.Hour), JanitorBatch)
	if err != nil {
		return res, err
	}
	res.AppSessions = int(n)

	unattached, err := model.ListUnattachedMedia(ctx, s.DB, now.Add(-24*time.Hour), JanitorBatch)
	if err != nil {
		return res, err
	}
	if err := s.removeMedia(ctx, unattached); err != nil {
		return res, err
	}
	res.UnattachedMedia = len(unattached)

	records, err := model.ListSoftDeletedRecords(ctx, s.DB, now.Add(-30*24*time.Hour), JanitorBatch)
	if err != nil {
		return res, err
	}
	if len(records) > 0 {
		uris := make([]string, 0, len(records))
		for _, r := range records {
			uris = append(uris, r.URI)
		}
		if _, err := model.DeleteRecordsByURIs(ctx, s.DB, uris); err != nil {
			return res, err
		}
	}
	res.Records = len(records)

	softMedia, err := model.ListSoftDeletedMedia(ctx, s.DB, now.Add(-30*24*time.Hour), JanitorBatch)
	if err != nil {
		return res, err
	}
	if err := s.removeMedia(ctx, softMedia); err != nil {
		return res, err
	}
	res.DeletedMedia = len(softMedia)

	n, err = model.DeleteOldNotifications(ctx, s.DB, now.Add(-90*24*time.Hour), JanitorBatch)
	if err != nil {
		return res, err
	}
	res.Notifications = int(n)

	// 予定は今日と明日の分だけを持つ（04 §4）。date が昨日以前になったら消す。
	n, err = model.DeleteScheduleEntriesBefore(ctx, s.DB, jstDateString(now), JanitorBatch)
	if err != nil {
		return res, err
	}
	res.ScheduleEntries = int(n)

	n, err = model.ExpireGroupInvites(ctx, s.DB, now)
	if err != nil {
		return res, err
	}
	res.InvitesExpired = int(n)

	n, err = model.DeleteExpiredGroupInvites(ctx, s.DB, now.Add(-30*24*time.Hour), JanitorBatch)
	if err != nil {
		return res, err
	}
	res.InvitesDeleted = int(n)

	stale, err := model.ListStaleGeoUsers(ctx, s.Redis, JanitorBatch)
	if err != nil {
		return res, err
	}
	if err := model.RemoveGeoUsers(ctx, s.Redis, stale); err != nil {
		return res, err
	}
	res.GeoUsers = len(stale)

	users, err := model.ListDeletedUsers(ctx, s.DB, now.Add(-30*24*time.Hour), JanitorBatch)
	if err != nil {
		return res, err
	}
	for _, u := range users {
		if err := s.purgeUser(ctx, u.DID); err != nil {
			return res, err
		}
		res.Users++
	}

	return res, nil
}

func (s *Janitor) removeMedia(ctx context.Context, rows []model.Media) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]string, 0, len(rows))
	for _, m := range rows {
		ids = append(ids, m.ID)
	}
	if _, err := model.DeleteMediaByIDs(ctx, s.DB, ids); err != nil {
		return err
	}
	s.deleteFiles(ctx, rows)
	return nil
}

func (s *Janitor) deleteFiles(ctx context.Context, rows []model.Media) {
	if s.Storage == nil {
		return
	}
	for _, m := range rows {
		if err := s.Storage.Delete(m.ID, m.CreatedAt); err != nil {
			slog.WarnContext(ctx, "media file delete failed", "err", err)
		}
	}
}

// purgeUser は deleted_at から 30 日たった users の行を消す（13 §5 の 6）。
func (s *Janitor) purgeUser(ctx context.Context, did string) error {
	return db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := s.reattachCreatedSpaces(ctx, tx, did); err != nil {
			return err
		}
		if _, err := model.DeleteGroupVotesByCreator(ctx, tx, did); err != nil {
			return err
		}
		return model.DeleteUserRow(ctx, tx, did)
	})
}

// reattachCreatedSpaces は created_by が消すユーザーを指す Space を、残るメンバーへ引き継ぐ。
// 残らなければ消す（spaces.created_by は CASCADE しないため）。
func (s *Janitor) reattachCreatedSpaces(ctx context.Context, q db.Querier, did string) error {
	spaces, err := model.ListSpacesCreatedBy(ctx, q, did)
	if err != nil {
		return err
	}
	for _, sp := range spaces {
		remaining, err := model.ListActiveSpaceMembers(ctx, q, sp.ID)
		if err != nil {
			return err
		}
		if len(remaining) == 0 {
			if err := model.DeleteSpace(ctx, q, sp.ID); err != nil {
				return err
			}
			continue
		}
		if err := model.SetSpaceCreatedBy(ctx, q, sp.ID, remaining[0].DID); err != nil {
			return err
		}
	}
	return nil
}
