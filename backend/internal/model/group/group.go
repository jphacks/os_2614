package group

import (
	"context"
	"database/sql"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/model"
)

// Group は groups の行（04 §2.4）。geofence と schedule をアクセス判定に使う（09 §4）。
type Group struct {
	SpaceID       string
	Description   string
	Visibility    string
	Origin        string
	Persistent    bool
	CenterLat     float64
	CenterLon     float64
	RadiusM       int
	ScheduleJSON  *string
	InviteQuorum  int
	ParentSpaceID *string
	ModLabel      string
}

func GetGroup(ctx context.Context, q db.Querier, spaceID string) (*Group, error) {
	return db.GetItem(ctx, q, `
		SELECT space_id, description, visibility, origin, persistent, center_lat, center_lon,
		       radius_m, schedule_json, invite_quorum, parent_space_id, mod_label
		FROM groups WHERE space_id = ?`,
		[]any{spaceID}, func(scan db.Scan, g *Group) error {
			var schedule, parent sql.NullString
			if err := scan(&g.SpaceID, &g.Description, &g.Visibility, &g.Origin, &g.Persistent,
				&g.CenterLat, &g.CenterLon, &g.RadiusM, &schedule, &g.InviteQuorum, &parent, &g.ModLabel); err != nil {
				return err
			}
			if schedule.Valid {
				g.ScheduleJSON = &schedule.String
			}
			if parent.Valid {
				g.ParentSpaceID = &parent.String
			}
			return nil
		})
}

// GroupWithSpace は groups の行に spaces の name / state を足したもの。一覧の絞り込みに使う。
type GroupWithSpace struct {
	Group
	Name  string
	State string
}

func InsertGroup(ctx context.Context, q db.Querier, g Group) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO groups (space_id, description, visibility, origin, persistent, center_lat, center_lon,
		  radius_m, schedule_json, invite_quorum, parent_space_id, mod_label)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.SpaceID, g.Description, g.Visibility, g.Origin, g.Persistent, g.CenterLat, g.CenterLon,
		g.RadiusM, model.NullableString(g.ScheduleJSON), g.InviteQuorum, model.NullableString(g.ParentSpaceID), g.ModLabel)
	return err
}

// UpdateGroupAdmin は name 以外の管理項目（description / schedule / invite_quorum / mod_label）を更新する（09 §9）。
func UpdateGroupAdmin(ctx context.Context, q db.Querier, spaceID, description string, scheduleJSON *string, inviteQuorum int, modLabel string) error {
	_, err := db.Execute(ctx, q, `
		UPDATE groups SET description = ?, schedule_json = ?, invite_quorum = ?, mod_label = ?
		WHERE space_id = ?`,
		description, model.NullableString(scheduleJSON), inviteQuorum, modLabel, spaceID)
	return err
}

// SetGroupVisibility は投票の可決で visibility を変える（09 §8.6）。
func SetGroupVisibility(ctx context.Context, q db.Querier, spaceID, visibility string) error {
	_, err := db.Execute(ctx, q, "UPDATE groups SET visibility = ? WHERE space_id = ?", visibility, spaceID)
	return err
}

// SetGroupPersistent は投票の可決で persistent を変える（09 §8.6）。
func SetGroupPersistent(ctx context.Context, q db.Querier, spaceID string, persistent bool) error {
	_, err := db.Execute(ctx, q, "UPDATE groups SET persistent = ? WHERE space_id = ?", persistent, spaceID)
	return err
}

func UpdateGroupParent(ctx context.Context, q db.Querier, spaceID string, parent *string) error {
	_, err := db.Execute(ctx, q, "UPDATE groups SET parent_space_id = ? WHERE space_id = ?",
		model.NullableString(parent), spaceID)
	return err
}

// ListActiveGroups は state='active' のグループを、中心・半径・schedule ごと返す。
// 重複の抑制（09 §2）と parent_space_id の計算に使う。
func ListActiveGroups(ctx context.Context, q db.Querier) ([]GroupWithSpace, error) {
	return db.GetItems(ctx, q, `
		SELECT g.space_id, g.description, g.visibility, g.origin, g.persistent, g.center_lat, g.center_lon,
		       g.radius_m, g.schedule_json, g.invite_quorum, g.parent_space_id, g.mod_label, s.name, s.state
		FROM groups g JOIN spaces s ON s.id = g.space_id
		WHERE s.state = 'active'`,
		nil, func(scan db.Scan, g *GroupWithSpace) error {
			var schedule, parent sql.NullString
			if err := scan(&g.SpaceID, &g.Description, &g.Visibility, &g.Origin, &g.Persistent,
				&g.CenterLat, &g.CenterLon, &g.RadiusM, &schedule, &g.InviteQuorum, &parent, &g.ModLabel,
				&g.Name, &g.State); err != nil {
				return err
			}
			if schedule.Valid {
				g.ScheduleJSON = &schedule.String
			}
			if parent.Valid {
				g.ParentSpaceID = &parent.String
			}
			return nil
		})
}
