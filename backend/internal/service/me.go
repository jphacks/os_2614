package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
)

// MeService は自分のプロフィールと設定（13 §1、14「自分」）。
type MeService struct {
	Deps
}

type Me struct {
	User     model.User
	Profile  model.Profile
	Settings model.UserSettings
}

func (s *MeService) Get(ctx context.Context, did string) (*Me, error) {
	u, err := model.GetUser(ctx, s.DB, did)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if u.DeletedAt != nil {
		return nil, httpx.ErrNotFound
	}

	me := &Me{User: *u}
	p, err := model.GetProfile(ctx, s.DB, did)
	switch {
	case err == nil:
		me.Profile = *p
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}

	if err := model.EnsureSettings(ctx, s.DB, did, s.Clock.Now()); err != nil {
		return nil, err
	}
	st, err := model.GetSettings(ctx, s.DB, did)
	if err != nil {
		return nil, err
	}
	me.Settings = *st
	return me, nil
}

// ContentScanConsentVersion は現在の投稿内容の自動判定への同意の版（02 §8）。
const ContentScanConsentVersion = 1

// SettingsUpdate は controller から渡される、来た項目だけの更新内容。
type SettingsUpdate struct {
	EncounterMode     *string
	EncounterDelaySec *int
	FriendBeaconShare *bool
	PartyAuto         *bool
	WarnDisplay       *string
	Notify            map[string]bool // nil = 変更なし。{} も「通知設定を変更する」を意味する
}

func (s *MeService) UpdateSettings(ctx context.Context, did string, u SettingsUpdate) (*model.UserSettings, error) {
	now := s.Clock.Now()
	if err := model.EnsureSettings(ctx, s.DB, did, now); err != nil {
		return nil, err
	}
	old, err := model.GetSettings(ctx, s.DB, did)
	if err != nil {
		return nil, err
	}

	patch := model.SettingsPatch{
		EncounterMode:     u.EncounterMode,
		EncounterDelaySec: u.EncounterDelaySec,
		FriendBeaconShare: u.FriendBeaconShare,
		PartyAuto:         u.PartyAuto,
		WarnDisplay:       u.WarnDisplay,
	}

	if u.Notify != nil {
		current, err := model.GetSettings(ctx, s.DB, did)
		if err != nil {
			return nil, err
		}
		merged := map[string]bool{}
		if current.NotifyJSON != "" {
			if err := json.Unmarshal([]byte(current.NotifyJSON), &merged); err != nil {
				return nil, err
			}
			if merged == nil {
				merged = map[string]bool{}
			}
		}
		for k, v := range u.Notify {
			merged[k] = v
		}
		for k, v := range merged {
			if v {
				delete(merged, k)
			}
		}
		b, err := json.Marshal(merged)
		if err != nil {
			return nil, err
		}
		raw := string(b)
		patch.NotifyJSON = &raw
	}

	if err := model.UpdateSettings(ctx, s.DB, did, patch, now); err != nil {
		return nil, err
	}
	next, err := model.GetSettings(ctx, s.DB, did)
	if err != nil {
		return nil, err
	}
	if IsStricterSettings(old, next) {
		if err := ApplyStrictEncounterSettings(ctx, s.Deps, did, next); err != nil {
			return nil, err
		}
	}
	return next, nil
}

// Consent は PUT /me/consents の結果。
type Consent struct {
	Version     int
	ConsentedAt *time.Time
}

func (s *MeService) UpdateConsent(ctx context.Context, did string, version int) (*Consent, error) {
	if version != 0 && version != ContentScanConsentVersion {
		return nil, httpx.ErrInvalidRequest.With("fields", map[string]string{"contentScan": "0 か 1 を指定してください"})
	}
	now := s.Clock.Now()
	if err := model.EnsureSettings(ctx, s.DB, did, now); err != nil {
		return nil, err
	}
	if version == 0 {
		if err := model.SetContentScanConsent(ctx, s.DB, did, nil, now); err != nil {
			return nil, err
		}
		return &Consent{Version: 0, ConsentedAt: nil}, nil
	}
	v := version
	if err := model.SetContentScanConsent(ctx, s.DB, did, &v, now); err != nil {
		return nil, err
	}
	return &Consent{Version: version, ConsentedAt: &now}, nil
}
