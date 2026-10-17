package controller

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service"
)

type MeController struct {
	Me *service.MeService
}

func (h *MeController) Register(r fiber.Router) {
	r.Get("/me", h.get)
	r.Patch("/me/settings", h.updateSettings)
	r.Put("/me/consents", h.updateConsent)
}

type meResponse struct {
	DID         string             `json:"did"`
	Handle      string             `json:"handle"`
	DisplayName string             `json:"displayName,omitempty"`
	Description string             `json:"description,omitempty"`
	AvatarURL   string             `json:"avatarUrl,omitempty"`
	BannerURL   string             `json:"bannerUrl,omitempty"`
	Status      string             `json:"status,omitempty"`
	Settings    meSettingsResponse `json:"settings"`
}

type meSettingsResponse struct {
	EncounterMode             string          `json:"encounterMode"`
	EncounterDelaySec         int             `json:"encounterDelaySec"`
	FriendBeaconShare         bool            `json:"friendBeaconShare"`
	PartyAuto                 bool            `json:"partyAuto"`
	LocationPaused            bool            `json:"locationPaused"`
	WarnDisplay               string          `json:"warnDisplay"`
	ContentScanConsentVersion *int            `json:"contentScanConsentVersion"`
	Notify                    map[string]bool `json:"notify"`
}

func decodeNotify(raw string) (map[string]bool, error) {
	m := map[string]bool{}
	if raw == "" {
		return m, nil
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]bool{}
	}
	return m, nil
}

func settingsResponse(st model.UserSettings) (meSettingsResponse, error) {
	notify, err := decodeNotify(st.NotifyJSON)
	if err != nil {
		return meSettingsResponse{}, err
	}
	return meSettingsResponse{
		EncounterMode:             st.EncounterMode,
		EncounterDelaySec:         st.EncounterDelaySec,
		FriendBeaconShare:         st.FriendBeaconShare,
		PartyAuto:                 st.PartyAuto,
		LocationPaused:            st.LocationPaused,
		WarnDisplay:               st.WarnDisplay,
		ContentScanConsentVersion: st.ContentScanConsentVersion,
		Notify:                    notify,
	}, nil
}

func (h *MeController) get(c fiber.Ctx) error {
	me, err := h.Me.Get(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	did := me.User.DID
	st, err := settingsResponse(me.Settings)
	if err != nil {
		return err
	}
	return c.JSON(meResponse{
		DID:         did,
		Handle:      me.User.Handle,
		DisplayName: me.Profile.DisplayName,
		Description: me.Profile.Description,
		AvatarURL:   atproto.AvatarURL(did, me.Profile.AvatarCID),
		BannerURL:   atproto.BannerURL(did, me.Profile.BannerCID),
		Status:      me.Profile.Status,
		Settings:    st,
	})
}

type settingsPatchRequest struct {
	EncounterMode     *string         `json:"encounterMode"`
	EncounterDelaySec *int            `json:"encounterDelaySec"`
	FriendBeaconShare *bool           `json:"friendBeaconShare"`
	PartyAuto         *bool           `json:"partyAuto"`
	WarnDisplay       *string         `json:"warnDisplay"`
	Notify            map[string]bool `json:"notify"`
}

var notifyTypes = map[string]bool{
	"friend.request":    true,
	"friend.accepted":   true,
	"party.confirmed":   true,
	"group.nearby":      true,
	"group.invited":     true,
	"group.suggested":   true,
	"encounter.daily":   true,
	"encounter.weekly":  true,
	"encounter.monthly": true,
	"moderation.hidden": true,
}

func (r *settingsPatchRequest) Validate() map[string]string {
	f := map[string]string{}
	if r.EncounterMode != nil {
		switch *r.EncounterMode {
		case "immediate", "delayed", "private":
		default:
			f["encounterMode"] = "immediate / delayed / private のいずれかを指定してください"
		}
	}
	if r.EncounterDelaySec != nil && (*r.EncounterDelaySec < 60 || *r.EncounterDelaySec > 604800) {
		f["encounterDelaySec"] = "60 秒から 604800 秒の範囲で指定してください"
	}
	if r.WarnDisplay != nil {
		switch *r.WarnDisplay {
		case "show", "blur", "hide":
		default:
			f["warnDisplay"] = "show / blur / hide のいずれかを指定してください"
		}
	}
	for k, v := range r.Notify {
		if !notifyTypes[k] {
			f["notify."+k] = "通知の種類が不正です"
			continue
		}
		if k == "moderation.hidden" && !v {
			f["notify."+k] = "この通知は無効にできません"
		}
	}
	if len(f) == 0 {
		return nil
	}
	return f
}

func (h *MeController) updateSettings(c fiber.Ctx) error {
	var req settingsPatchRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	st, err := h.Me.UpdateSettings(c.Context(), httpx.DID(c), service.SettingsUpdate{
		EncounterMode:     req.EncounterMode,
		EncounterDelaySec: req.EncounterDelaySec,
		FriendBeaconShare: req.FriendBeaconShare,
		PartyAuto:         req.PartyAuto,
		WarnDisplay:       req.WarnDisplay,
		Notify:            req.Notify,
	})
	if err != nil {
		return err
	}
	resp, err := settingsResponse(*st)
	if err != nil {
		return err
	}
	return c.JSON(resp)
}

type consentRequest struct {
	ContentScan *int `json:"contentScan"`
}

func (r *consentRequest) Validate() map[string]string {
	if r.ContentScan == nil {
		return map[string]string{"contentScan": "必須です"}
	}
	if *r.ContentScan != 0 && *r.ContentScan != service.ContentScanConsentVersion {
		return map[string]string{"contentScan": "0 か 1 を指定してください"}
	}
	return nil
}

type consentResponse struct {
	ContentScan int        `json:"contentScan"`
	ConsentedAt *time.Time `json:"consentedAt"`
}

func (h *MeController) updateConsent(c fiber.Ctx) error {
	var req consentRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	res, err := h.Me.UpdateConsent(c.Context(), httpx.DID(c), *req.ContentScan)
	if err != nil {
		return err
	}
	return c.JSON(consentResponse{ContentScan: res.Version, ConsentedAt: res.ConsentedAt})
}
