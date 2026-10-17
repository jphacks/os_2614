package controller

import (
	"encoding/json"
	"io"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	core "github.com/applemango/chain/internal/service/core"
	mesvc "github.com/applemango/chain/internal/service/me"
	"github.com/applemango/chain/internal/utils"
)

type MeController struct {
	Me *mesvc.MeService
}

func (h *MeController) Register(r fiber.Router) {
	r.Get("/me", h.get)
	r.Patch("/me/settings", h.updateSettings)
	r.Put("/me/consents", h.updateConsent)
	r.Put("/me/status", h.updateStatus)
	r.Patch("/me/profile", h.updateProfile)
	r.Put("/me/avatar", h.updateAvatar)
	r.Delete("/me/avatar", h.deleteAvatar)
	r.Put("/me/banner", h.updateBanner)
	r.Delete("/me/banner", h.deleteBanner)
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
	NaturalGroups             bool            `json:"naturalGroups"`
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
		NaturalGroups:             st.NaturalGroups,
		LocationPaused:            st.LocationPaused,
		WarnDisplay:               st.WarnDisplay,
		ContentScanConsentVersion: st.ContentScanConsentVersion,
		Notify:                    notify,
	}, nil
}

// meResponse は GET /me と同じ形を組み立てる。プロフィール編集の API もこれで返す（13 §1.3）。
func (h *MeController) meResponse(me *mesvc.Me) (meResponse, error) {
	st, err := settingsResponse(me.Settings)
	if err != nil {
		return meResponse{}, err
	}
	did := me.User.DID
	return meResponse{
		DID:         did,
		Handle:      me.User.Handle,
		DisplayName: me.Profile.DisplayName,
		Description: me.Profile.Description,
		AvatarURL:   atproto.AvatarURL(did, me.Profile.AvatarCID),
		BannerURL:   atproto.BannerURL(did, me.Profile.BannerCID),
		Status:      me.Profile.Status,
		Settings:    st,
	}, nil
}

func (h *MeController) get(c fiber.Ctx) error {
	me, err := h.Me.Get(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	resp, err := h.meResponse(me)
	if err != nil {
		return err
	}
	return c.JSON(resp)
}

type settingsPatchRequest struct {
	EncounterMode     *string         `json:"encounterMode"`
	EncounterDelaySec *int            `json:"encounterDelaySec"`
	FriendBeaconShare *bool           `json:"friendBeaconShare"`
	PartyAuto         *bool           `json:"partyAuto"`
	NaturalGroups     *bool           `json:"naturalGroups"`
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
	st, err := h.Me.UpdateSettings(c.Context(), httpx.DID(c), mesvc.SettingsUpdate{
		EncounterMode:     req.EncounterMode,
		EncounterDelaySec: req.EncounterDelaySec,
		FriendBeaconShare: req.FriendBeaconShare,
		PartyAuto:         req.PartyAuto,
		NaturalGroups:     req.NaturalGroups,
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
	if *r.ContentScan != 0 && *r.ContentScan != core.ContentScanConsentVersion {
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

type statusRequest struct {
	Status string `json:"status"`
}

func (r *statusRequest) Validate() map[string]string {
	if utf8.RuneCountInString(r.Status) > 40 {
		return map[string]string{"status": "40 文字までです"}
	}
	return nil
}

type statusResponse struct {
	Status string `json:"status"`
}

func (h *MeController) updateStatus(c fiber.Ctx) error {
	var req statusRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	p, err := h.Me.UpdateStatus(c.Context(), httpx.DID(c), httpx.SessionID(c), req.Status)
	if err != nil {
		return err
	}
	return c.JSON(statusResponse{Status: p.Status})
}

type profilePatchRequest struct {
	DisplayName *string `json:"displayName"`
	Description *string `json:"description"`
}

func (r *profilePatchRequest) Validate() map[string]string {
	f := map[string]string{}
	if r.DisplayName != nil && utils.GraphemeCount(*r.DisplayName) > mesvc.MaxDisplayNameGraphemes {
		f["displayName"] = "64 文字までです"
	}
	if r.Description != nil && utils.GraphemeCount(*r.Description) > mesvc.MaxProfileDescriptionGraphemes {
		f["description"] = "256 文字までです"
	}
	if len(f) == 0 {
		return nil
	}
	return f
}

func (h *MeController) updateProfile(c fiber.Ctx) error {
	var req profilePatchRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	me, err := h.Me.UpdateProfile(c.Context(), httpx.DID(c), httpx.SessionID(c), mesvc.ProfileUpdate{
		DisplayName: req.DisplayName,
		Description: req.Description,
	})
	if err != nil {
		return err
	}
	return h.respondMe(c, me)
}

func (h *MeController) updateAvatar(c fiber.Ctx) error {
	return h.updateProfileImage(c, true)
}

func (h *MeController) updateBanner(c fiber.Ctx) error {
	return h.updateProfileImage(c, false)
}

func (h *MeController) updateProfileImage(c fiber.Ctx, avatar bool) error {
	file, size, err := profileImageFile(c)
	if err != nil {
		return err
	}
	defer file.Close()

	did, sid := httpx.DID(c), httpx.SessionID(c)
	var me *mesvc.Me
	if avatar {
		me, err = h.Me.UpdateAvatar(c.Context(), did, sid, file, size)
	} else {
		me, err = h.Me.UpdateBanner(c.Context(), did, sid, file, size)
	}
	if err != nil {
		return err
	}
	return h.respondMe(c, me)
}

func (h *MeController) deleteAvatar(c fiber.Ctx) error {
	me, err := h.Me.DeleteAvatar(c.Context(), httpx.DID(c), httpx.SessionID(c))
	if err != nil {
		return err
	}
	return h.respondMe(c, me)
}

func (h *MeController) deleteBanner(c fiber.Ctx) error {
	me, err := h.Me.DeleteBanner(c.Context(), httpx.DID(c), httpx.SessionID(c))
	if err != nil {
		return err
	}
	return h.respondMe(c, me)
}

func (h *MeController) respondMe(c fiber.Ctx, me *mesvc.Me) error {
	resp, err := h.meResponse(me)
	if err != nil {
		return err
	}
	return c.JSON(resp)
}

func profileImageFile(c fiber.Ctx) (io.ReadCloser, int64, error) {
	header, err := c.FormFile("file")
	if err != nil {
		return nil, 0, httpx.ErrInvalidRequest.With("fields", map[string]string{"file": "file が必要です"})
	}
	f, err := header.Open()
	if err != nil {
		return nil, 0, httpx.ErrInvalidRequest.With("fields", map[string]string{"file": "file を開けません"})
	}
	return f, header.Size, nil
}
