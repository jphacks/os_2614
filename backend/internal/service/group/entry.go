package group

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	spacemodel "github.com/applemango/chain/internal/model/space"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/notify"
	"github.com/applemango/chain/internal/service/proximity"
	"github.com/applemango/chain/internal/service/schedule"
)

// GroupEntryService はエリア入場通知（09 §6.1）。位置の保存後フック（proximity.LocationHooks）として
// 非同期で呼ばれ、その位置を含むグループのうち通知すべき最小の 1 件に group.nearby を積む。
type GroupEntryService struct {
	core.Deps
	Broker realtime.Broker
}

// groupNearbyTTL は入場通知の送信済み印（gn:）の保持時間（09 §6.1、04 §3）。
const groupNearbyTTL = 24 * time.Hour

// nearbyPayload は group.nearby の payload（12 §3）。spaceId は FCM の collapse_key を
// Space ごとにするためにも使う（12 §4）。
type nearbyPayload struct {
	Text    string `json:"text"`
	SpaceID string `json:"spaceId"`
}

// LocationUpdated は位置の保存後のフック。失敗しても位置の更新は成功しているので、ログだけ残す。
func (s *GroupEntryService) LocationUpdated(ctx context.Context, did string, loc model.Location) {
	if err := s.notifyEntry(ctx, did, loc); err != nil {
		slog.WarnContext(ctx, "group entry notify failed", "err", err)
	}
}

// notifyEntry は 09 §6.1 の判定。1 回の位置更新で通知するのは、条件を満たすうち半径が
// 最小の 1 件だけ。送信済み印（gn:）を 24 時間立てる。
func (s *GroupEntryService) notifyEntry(ctx context.Context, did string, loc model.Location) error {
	now := s.Clock.Now()
	candidates, err := searchGroupsNear(ctx, s.Redis, loc.Lat, loc.Lon, groupSearchRadiusM)
	if err != nil {
		return err
	}

	var chosen *spacemodel.Space
	chosenRadius := 0
	for _, c := range candidates {
		sp, group, ok, err := lookupGroup(ctx, s.DB, c.Name)
		if err != nil {
			return err
		}
		if !ok || sp.State != "active" {
			continue
		}
		// 自動グループ（origin=natural）は自動参加なので通知しない（09 §6.1、§8）。
		if group.Visibility != "open" || group.Origin == "natural" {
			continue
		}
		if !schedule.ScheduleActive(group.ScheduleJSON, now) {
			continue
		}
		zone := proximity.Zone{ID: sp.ID, CenterLat: group.CenterLat, CenterLon: group.CenterLon, RadiusM: group.RadiusM}
		if !proximity.InsideZone(zone, &loc) {
			continue
		}
		// 既にメンバーなら通知しない。退出したことがある（left_at がある）人にも通知しない。
		if _, err := spacemodel.GetSpaceMember(ctx, s.DB, sp.ID, did); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		notified, err := s.Redis.Exists(ctx, kv.GroupNotifiedKey(did, sp.ID)).Result()
		if err != nil {
			return err
		}
		if notified > 0 {
			continue
		}
		if chosen == nil || group.RadiusM < chosenRadius {
			chosen, chosenRadius = sp, group.RadiusM
		}
	}
	if chosen == nil {
		return nil
	}

	// 位置の更新がほぼ同時に届くと、2 つの呼び出しが両方とも上の Exists を通りうる。
	// 印を SetNX で先に取った方だけが通知を積み、二重の通知を防ぐ。
	claimed, err := s.Redis.SetNX(ctx, kv.GroupNotifiedKey(did, chosen.ID), "1", groupNearbyTTL).Result()
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	item, err := notify.EnqueueNotification(ctx, s.DB, s.Deps, did, notify.NotificationGroupNearby, nearbyPayload{
		Text: "近くに『" + chosen.Name + "』があります", SpaceID: chosen.ID,
	}, now)
	if err != nil {
		return err
	}
	if item != nil && s.Broker != nil {
		s.Broker.Publish(ctx, realtime.Event{Name: "notification", User: did, Data: *item})
	}
	return nil
}
