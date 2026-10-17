package model

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

type UserSettings struct {
	DID                       string
	EncounterMode             string
	EncounterDelaySec         int
	FriendBeaconShare         bool
	PartyAuto                 bool
	NaturalGroups             bool
	LocationPaused            bool
	WarnDisplay               string
	NotifyJSON                string
	ContentScanConsentVersion *int
}

// EnsureSettings は既定値の行がなければ作る。
func EnsureSettings(ctx context.Context, q db.Querier, did string, now time.Time) error {
	_, err := db.Execute(ctx, q,
		"INSERT INTO user_settings (did, updated_at) VALUES (?, ?) ON CONFLICT (did) DO NOTHING",
		did, clock.UnixMilli(now))
	return err
}

// SettingsPatch は PATCH /me/settings で来た項目だけを持つ。nil の項目は更新しない。
type SettingsPatch struct {
	EncounterMode     *string
	EncounterDelaySec *int
	FriendBeaconShare *bool
	PartyAuto         *bool
	NaturalGroups     *bool
	WarnDisplay       *string
	NotifyJSON        *string
}

// UpdateSettings は patch に含まれる列と updated_at を更新する。
func UpdateSettings(ctx context.Context, q db.Querier, did string, patch SettingsPatch, now time.Time) error {
	sets := make([]string, 0, 8)
	args := make([]any, 0, 9)
	if patch.EncounterMode != nil {
		sets = append(sets, "encounter_mode = ?")
		args = append(args, *patch.EncounterMode)
	}
	if patch.EncounterDelaySec != nil {
		sets = append(sets, "encounter_delay_sec = ?")
		args = append(args, *patch.EncounterDelaySec)
	}
	if patch.FriendBeaconShare != nil {
		sets = append(sets, "friend_beacon_share = ?")
		args = append(args, *patch.FriendBeaconShare)
	}
	if patch.PartyAuto != nil {
		sets = append(sets, "party_auto = ?")
		args = append(args, *patch.PartyAuto)
	}
	if patch.NaturalGroups != nil {
		sets = append(sets, "natural_groups = ?")
		args = append(args, *patch.NaturalGroups)
	}
	if patch.WarnDisplay != nil {
		sets = append(sets, "warn_display = ?")
		args = append(args, *patch.WarnDisplay)
	}
	if patch.NotifyJSON != nil {
		sets = append(sets, "notify_json = ?")
		args = append(args, *patch.NotifyJSON)
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, clock.UnixMilli(now), did)

	_, err := db.Execute(ctx, q,
		"UPDATE user_settings SET "+strings.Join(sets, ", ")+" WHERE did = ?", args...)
	return err
}

// SetContentScanConsent は同意の版と時刻を保存する。version が nil なら両方 NULL にする（撤回）。
func SetContentScanConsent(ctx context.Context, q db.Querier, did string, version *int, now time.Time) error {
	var versionArg, atArg any
	if version != nil {
		versionArg = *version
		atArg = clock.UnixMilli(now)
	}
	_, err := db.Execute(ctx, q,
		"UPDATE user_settings SET content_scan_consent_version = ?, content_scan_consent_at = ?, updated_at = ? WHERE did = ?",
		versionArg, atArg, clock.UnixMilli(now), did)
	return err
}

func GetSettings(ctx context.Context, q db.Querier, did string) (*UserSettings, error) {
	return db.GetItem(ctx, q, `
		SELECT did, encounter_mode, encounter_delay_sec, friend_beacon_share, party_auto, natural_groups,
		       location_paused, warn_display, notify_json, content_scan_consent_version
		FROM user_settings WHERE did = ?`,
		[]any{did}, func(scan db.Scan, s *UserSettings) error {
			var consent sql.NullInt64
			if err := scan(&s.DID, &s.EncounterMode, &s.EncounterDelaySec, &s.FriendBeaconShare, &s.PartyAuto,
				&s.NaturalGroups, &s.LocationPaused, &s.WarnDisplay, &s.NotifyJSON, &consent); err != nil {
				return err
			}
			if consent.Valid {
				v := int(consent.Int64)
				s.ContentScanConsentVersion = &v
			}
			return nil
		})
}
