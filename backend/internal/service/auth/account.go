package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	maintenancemodel "github.com/applemango/chain/internal/model/maintenance"
	spacemodel "github.com/applemango/chain/internal/model/space"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/storage"
)

// AccountService はアカウント削除（13 §5）。
type AccountService struct {
	core.Deps
	OAuth   OAuthClient
	Storage *storage.Store
}

// Delete は 13 §5 の 2〜6 を行う。SQLite の削除・更新は 1 トランザクションで行い、
// コミット後に Redis・メディアのファイル・ATProto セッションの取り消しを行う。
func (s *AccountService) Delete(ctx context.Context, did string, deletePdsRecords bool) error {
	if deletePdsRecords {
		return httpx.ErrInvalidRequest.With("reason", "deletePdsRecords=true には未対応です")
	}
	u, err := model.GetUser(ctx, s.DB, did)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if u.DeletedAt != nil {
		return httpx.ErrNotFound
	}

	now := s.Clock.Now()
	var (
		files   []spacemodel.Media
		deleted []spacemodel.Space
		sids    []string
		atpIDs  []string
	)

	err = db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		memberships, err := maintenancemodel.ListMembershipsByDID(ctx, tx, did)
		if err != nil {
			return err
		}
		createdByMe, err := maintenancemodel.ListSpacesCreatedBy(ctx, tx, did)
		if err != nil {
			return err
		}
		own, err := maintenancemodel.ListMediaByOwner(ctx, tx, did)
		if err != nil {
			return err
		}
		files = append(files, own...)

		if _, err := maintenancemodel.DeleteSpaceMembersByDID(ctx, tx, did); err != nil {
			return err
		}

		toDelete := map[string]bool{}
		// 退出していない Space について、owner を引き継ぎ、最後の 1 人なら archived にする（09 §5）。
		for _, m := range memberships {
			if m.LeftAt != nil {
				continue
			}
			remaining, err := spacemodel.ListActiveSpaceMembers(ctx, tx, m.SpaceID)
			if err != nil {
				return err
			}
			switch {
			case len(remaining) == 0:
				if err := archiveSpace(ctx, tx, m.SpaceID, now); err != nil {
					return err
				}
				toDelete[m.SpaceID] = true
			case m.Role == "owner":
				if err := spacemodel.SetSpaceMemberRole(ctx, tx, m.SpaceID, remaining[0].DID, "owner"); err != nil {
					return err
				}
			}
		}

		// created_by が自分な Space は、残るメンバーへ引き継ぐ。残らなければ消す（spaces.created_by は CASCADE しない）。
		for _, sp := range createdByMe {
			if toDelete[sp.ID] {
				continue
			}
			remaining, err := spacemodel.ListActiveSpaceMembers(ctx, tx, sp.ID)
			if err != nil {
				return err
			}
			if len(remaining) == 0 {
				if err := archiveSpace(ctx, tx, sp.ID, now); err != nil {
					return err
				}
				toDelete[sp.ID] = true
				continue
			}
			if err := maintenancemodel.SetSpaceCreatedBy(ctx, tx, sp.ID, remaining[0].DID); err != nil {
				return err
			}
		}

		// メンバーが 0 人になった archived の Space は Space ごと消す（13 §2）。
		if len(toDelete) > 0 {
			ids := make([]string, 0, len(toDelete))
			for id := range toDelete {
				ids = append(ids, id)
			}
			inSpace, err := maintenancemodel.ListMediaInSpaces(ctx, tx, ids)
			if err != nil {
				return err
			}
			files = append(files, inSpace...)
			for _, id := range ids {
				sp, err := spacemodel.GetSpace(ctx, tx, id)
				if err != nil {
					return err
				}
				deleted = append(deleted, *sp)
			}
			if _, err := maintenancemodel.DeleteSpacesByIDs(ctx, tx, ids); err != nil {
				return err
			}
		}

		// 自分の投稿した record とメディア（13 §5 の 4）。
		if _, err := maintenancemodel.DeleteRecordsByAuthor(ctx, tx, did); err != nil {
			return err
		}
		if _, err := maintenancemodel.DeleteMediaByOwner(ctx, tx, did); err != nil {
			return err
		}

		// 物理削除（13 §5 の 3）。users の行は残すので、CASCADE に頼らずここで消す。
		deleters := []func(context.Context, db.Querier, string) (int64, error){
			maintenancemodel.DeleteBeaconTokensByDID,
			maintenancemodel.DeleteEncountersByDID,
			maintenancemodel.DeleteFriendshipsByDID,
			maintenancemodel.DeleteFollowsByDID,
			maintenancemodel.DeleteFriendInvitesByDID,
			maintenancemodel.DeleteLocationSharesByDID,
			maintenancemodel.DeleteAreasByDID,
			maintenancemodel.DeleteDevicesByDID,
			maintenancemodel.DeleteNotificationsByDID,
			maintenancemodel.DeleteBlocksByDID,
			maintenancemodel.DeleteUserSettings,
			maintenancemodel.DeletePublicPostsByAuthor,
			maintenancemodel.DeleteGroupInvitesByDID,
			maintenancemodel.DeleteGroupInviteApprovalsByDID,
			maintenancemodel.DeleteGroupVoteBallotsByDID,
			maintenancemodel.DeleteGroupVotesByCreator,
		}
		for _, del := range deleters {
			if _, err := del(ctx, tx, did); err != nil {
				return err
			}
		}

		// アプリセッションをすべて失効させ、ATProto セッションも取り消す（13 §5 の 2）。
		if sids, err = maintenancemodel.ListAppSessionIDsByDID(ctx, tx, did); err != nil {
			return err
		}
		if _, err := maintenancemodel.RevokeAllAppSessionsByDID(ctx, tx, did, now); err != nil {
			return err
		}
		if atpIDs, err = maintenancemodel.ListATProtoSessionIDsByDID(ctx, tx, did); err != nil {
			return err
		}
		if _, err := maintenancemodel.RevokeAllATProtoSessionsByDID(ctx, tx, did, now); err != nil {
			return err
		}

		// users.deleted_at を立て、audit_log の actor_did を NULL にし、account.delete を残す（13 §5 の 5）。
		if _, err := maintenancemodel.MarkUserDeleted(ctx, tx, did, now); err != nil {
			return err
		}
		if _, err := maintenancemodel.NullAuditActor(ctx, tx, did); err != nil {
			return err
		}
		return model.InsertAuditLog(ctx, tx, model.AuditEntry{
			ID: s.IDs.NewID(), Action: "account.delete", CreatedAt: now,
		})
	})
	if err != nil {
		return err
	}

	s.cancelATProtoSessions(ctx, did, atpIDs)
	s.cleanupRedis(ctx, did, deleted, sids)
	s.deleteFiles(ctx, files)
	return nil
}

func archiveSpace(ctx context.Context, q db.Querier, spaceID string, now time.Time) error {
	return spacemodel.SetSpaceState(ctx, q, spaceID, "archived", &now)
}

// cancelATProtoSessions は ATProto 側のトークンを取り消す。失敗しても削除は続ける（13 §5 の 2）。
func (s *AccountService) cancelATProtoSessions(ctx context.Context, did string, ids []string) {
	if s.OAuth == nil {
		return
	}
	for _, id := range ids {
		if err := s.OAuth.Logout(ctx, syntax.DID(did), id); err != nil {
			slog.WarnContext(ctx, "atproto logout failed", "did", did, "err", err)
		}
	}
}

// cleanupRedis はコミット後に揮発データと索引を消す。失敗しても削除は続ける。
func (s *AccountService) cleanupRedis(ctx context.Context, did string, deleted []spacemodel.Space, sids []string) {
	warn := func(err error) {
		if err != nil {
			slog.WarnContext(ctx, "redis cleanup failed", "did", did, "err", err)
		}
	}
	warn(s.Redis.ZRem(ctx, kv.GeoUsersKey(), did).Err())
	warn(s.Redis.Del(ctx, kv.LocationKey(did), kv.BleRecentKey(did), kv.PartyOfKey(did)).Err())
	s.deleteByPattern(ctx, "sgt:"+did+":*")
	s.deleteByPattern(ctx, "sgt:*:"+did+":*")
	s.deleteByPattern(ctx, "pe:"+did+":*")
	s.deleteByPattern(ctx, "pe:*:"+did)
	s.deleteByPattern(ctx, "acc:*:"+did+":*")
	s.removePeerActive(ctx, did)

	for _, sp := range deleted {
		switch sp.Type {
		case core.SpaceTypeGroup:
			warn(s.Redis.ZRem(ctx, kv.GeoGroupsKey(), sp.ID).Err())
		case core.SpaceTypeParty:
			warn(model.DeletePartyPresence(ctx, s.Redis, sp.ID))
			warn(model.DeletePartyLeavers(ctx, s.Redis, sp.ID))
		}
	}
	for _, sid := range sids {
		warn(model.SetSessionCache(ctx, s.Redis, sid, false))
	}
}

func (s *AccountService) deleteByPattern(ctx context.Context, pattern string) {
	iter := s.Redis.Scan(ctx, 0, pattern, 200).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		slog.WarnContext(ctx, "redis scan failed", "err", err)
		return
	}
	if len(keys) == 0 {
		return
	}
	if err := s.Redis.Del(ctx, keys...).Err(); err != nil {
		slog.WarnContext(ctx, "redis delete failed", "err", err)
	}
}

func (s *AccountService) removePeerActive(ctx context.Context, did string) {
	members, err := s.Redis.ZRange(ctx, kv.PeerActiveKey(), 0, -1).Result()
	if err != nil {
		slog.WarnContext(ctx, "redis scan failed", "err", err)
		return
	}
	prefix, suffix := did+"|", "|"+did
	stale := make([]any, 0, len(members))
	for _, m := range members {
		if strings.HasPrefix(m, prefix) || strings.HasSuffix(m, suffix) {
			stale = append(stale, m)
		}
	}
	if len(stale) == 0 {
		return
	}
	if err := s.Redis.ZRem(ctx, kv.PeerActiveKey(), stale...).Err(); err != nil {
		slog.WarnContext(ctx, "redis delete failed", "err", err)
	}
}

func (s *AccountService) deleteFiles(ctx context.Context, files []spacemodel.Media) {
	if s.Storage == nil {
		return
	}
	seen := make(map[string]bool, len(files))
	for _, m := range files {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		if err := s.Storage.Delete(m.ID, m.CreatedAt); err != nil {
			slog.WarnContext(ctx, "media file delete failed", "err", err)
		}
	}
}
