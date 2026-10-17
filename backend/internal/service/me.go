package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
)

// MeService は自分のプロフィールと設定（13 §1、14「自分」）。
type MeService struct {
	Deps
	Moderator Moderator
	Writer    RecordWriter
}

// RecordWriter はユーザーの PDS に record を読み書きする（03 §4）。実装は atproto.Writer、テストでは fake。
type RecordWriter interface {
	PutRecord(ctx context.Context, did, atprotoSessionID, collection, rkey string, record any, swapRecord string) (uri, cid string, err error)
	GetRecord(ctx context.Context, did, atprotoSessionID, collection, rkey string) (value map[string]any, cid string, found bool, err error)
	UploadBlob(ctx context.Context, did, atprotoSessionID, mime string, data []byte) (*atproto.Blob, error)
	CreateRecord(ctx context.Context, did, atprotoSessionID, collection string, record any) (uri, cid string, err error)
	DeleteRecord(ctx context.Context, did, atprotoSessionID, collection, rkey string) error
}

// collectionActorProfile は CHAIN 固有のプロフィール（03 §6）。rkey は literal:self。
// collectionBskyProfile（jetstream.go）は Bluesky のプロフィールで、CHAIN からも編集する（13 §1.3）。
const (
	collectionActorProfile = "jp.i32.chain.actor.profile"
	actorProfileRKey       = "self"
	bskyProfileRKey        = "self"
	mimeJPEG               = "image/jpeg"
)

// ContentKindProfile はプロフィールの displayName / description の判定種別（11 §2、13 §1.3）。
const ContentKindProfile = "profile"

// displayName / description の上限（13 §1.3、Bluesky の lexicon に合わせる）。
const (
	MaxDisplayNameGraphemes        = 64
	MaxProfileDescriptionGraphemes = 256
)

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
	NaturalGroups     *bool
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
		NaturalGroups:     u.NaturalGroups,
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

// UpdateStatus はステータスを PDS に書いてから profiles を更新する（13 §1.1）。
// 空文字ならステータスを消す（record は残し、status を空にする）。
// PDS のセッションが失効していれば 401 atproto_session_expired、その他のエラーは 502 upstream_error。
func (s *MeService) UpdateStatus(ctx context.Context, did, sid, status string) (*model.Profile, error) {
	if s.Moderator != nil {
		v, err := s.Moderator.Judge(ctx, Content{Kind: ContentKindStatus, Text: status})
		if err != nil {
			slog.WarnContext(ctx, "moderation error; treating as ok", "err", err)
		} else if v.Label == LabelBlock {
			return nil, httpx.ErrModerationBlocked.With("categories", v.Categories)
		}
	}
	if s.Writer == nil {
		return nil, httpx.ErrUpstream.With("reason", "PDS への書き込みができません")
	}
	sess, err := model.GetAppSession(ctx, s.DB, sid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrATProtoSession
	}
	if err != nil {
		return nil, err
	}

	now := s.Clock.Now()
	record := map[string]any{
		"$type":     collectionActorProfile,
		"status":    status,
		"createdAt": formatDateTime(now),
	}
	if _, _, err := s.Writer.PutRecord(ctx, did, sess.ATProtoSessionID, collectionActorProfile, actorProfileRKey, record, ""); err != nil {
		return nil, profileWriteError(err)
	}
	if err := model.SetProfileStatus(ctx, s.DB, did, status, now); err != nil {
		return nil, err
	}
	return model.GetProfile(ctx, s.DB, did)
}

// ProfileUpdate は PATCH /me/profile で来た項目だけを持つ（13 §1.3）。nil は変更なし。
type ProfileUpdate struct {
	DisplayName *string
	Description *string
}

// UpdateProfile は Bluesky のプロフィールを書き換える（13 §1.3）。
// 今の record を読んで来た項目だけを差し替え、知らない項目は保つ。record が無ければ作る。
// 読み込み時の cid を swapRecord として渡すので、その間に変更されていれば 409 conflict になる。
// 成功したら profiles を更新し、/me と同じ形を返すために読み直す。
func (s *MeService) UpdateProfile(ctx context.Context, did, sid string, in ProfileUpdate) (*Me, error) {
	if in.DisplayName == nil && in.Description == nil {
		return nil, httpx.ErrInvalidRequest.With("reason", "変更する項目がありません")
	}
	if err := s.moderateProfileFields(ctx, in); err != nil {
		return nil, err
	}
	atsess, err := s.atprotoSession(ctx, sid)
	if err != nil {
		return nil, err
	}

	record, cid, err := s.readProfileRecord(ctx, did, atsess)
	if err != nil {
		return nil, err
	}
	record["$type"] = collectionBskyProfile
	if in.DisplayName != nil {
		setOrDeleteString(record, "displayName", *in.DisplayName)
	}
	if in.Description != nil {
		setOrDeleteString(record, "description", *in.Description)
	}
	if _, _, err := s.writeProfileRecord(ctx, did, atsess, record, cid); err != nil {
		return nil, err
	}

	now := s.Clock.Now()
	if err := model.SetProfileText(ctx, s.DB, did, stringField(record, "displayName"), stringField(record, "description"), now); err != nil {
		return nil, err
	}
	return s.Get(ctx, did)
}

// UpdateAvatar はアイコンを差し替える（13 §1.3）。再エンコードした JPEG を uploadBlob し、record の avatar を入れ替える。
func (s *MeService) UpdateAvatar(ctx context.Context, did, sid string, r io.Reader, size int64) (*Me, error) {
	return s.updateProfileImage(ctx, did, sid, "avatar", avatarMaxWidth, avatarMaxHeight, r, size)
}

// UpdateBanner はバナーを差し替える（13 §1.3）。規則は UpdateAvatar と同じで、枠だけが違う。
func (s *MeService) UpdateBanner(ctx context.Context, did, sid string, r io.Reader, size int64) (*Me, error) {
	return s.updateProfileImage(ctx, did, sid, "banner", bannerMaxWidth, bannerMaxHeight, r, size)
}

// DeleteAvatar はアイコンを record から消し、profiles.avatar_cid を空に戻す（13 §1.3）。
func (s *MeService) DeleteAvatar(ctx context.Context, did, sid string) (*Me, error) {
	return s.deleteProfileImage(ctx, did, sid, "avatar")
}

// DeleteBanner はバナーを record から消し、profiles.banner_cid を空に戻す（13 §1.3）。
func (s *MeService) DeleteBanner(ctx context.Context, did, sid string) (*Me, error) {
	return s.deleteProfileImage(ctx, did, sid, "banner")
}

func (s *MeService) updateProfileImage(ctx context.Context, did, sid, field string, maxW, maxH int, r io.Reader, size int64) (*Me, error) {
	if s.Writer == nil {
		return nil, httpx.ErrUpstream.With("reason", "PDS への書き込みができません")
	}
	img, err := decodeProfileImage(r, size)
	if err != nil {
		return nil, err
	}
	jpegBytes, err := encodeProfileJPEG(img, maxW, maxH)
	if err != nil {
		return nil, err
	}
	atsess, err := s.atprotoSession(ctx, sid)
	if err != nil {
		return nil, err
	}
	blob, err := s.Writer.UploadBlob(ctx, did, atsess, mimeJPEG, jpegBytes)
	if err != nil {
		return nil, profileWriteError(err)
	}

	record, cid, err := s.readProfileRecord(ctx, did, atsess)
	if err != nil {
		return nil, err
	}
	record["$type"] = collectionBskyProfile
	record[field] = blob
	if _, _, err := s.writeProfileRecord(ctx, did, atsess, record, cid); err != nil {
		return nil, err
	}

	now := s.Clock.Now()
	if err := s.setProfileCID(ctx, did, field, blob.Ref.Link, now); err != nil {
		return nil, err
	}
	return s.Get(ctx, did)
}

func (s *MeService) deleteProfileImage(ctx context.Context, did, sid, field string) (*Me, error) {
	if s.Writer == nil {
		return nil, httpx.ErrUpstream.With("reason", "PDS への書き込みができません")
	}
	atsess, err := s.atprotoSession(ctx, sid)
	if err != nil {
		return nil, err
	}
	record, cid, found, err := s.Writer.GetRecord(ctx, did, atsess, collectionBskyProfile, bskyProfileRKey)
	if err != nil {
		return nil, profileWriteError(err)
	}
	if found && record != nil {
		if _, ok := record[field]; ok {
			delete(record, field)
			if _, _, err := s.writeProfileRecord(ctx, did, atsess, record, cid); err != nil {
				return nil, err
			}
		}
	}

	now := s.Clock.Now()
	if err := s.setProfileCID(ctx, did, field, "", now); err != nil {
		return nil, err
	}
	return s.Get(ctx, did)
}

// moderateProfileFields は displayName / description を書く前に判定する（13 §1.3、11 §2）。
// 空文字（項目を消す）は判定しない。block なら何も保存せず 422 を返す。
func (s *MeService) moderateProfileFields(ctx context.Context, in ProfileUpdate) error {
	if s.Moderator == nil {
		return nil
	}
	for _, v := range []*string{in.DisplayName, in.Description} {
		if v == nil || *v == "" {
			continue
		}
		verdict, err := s.Moderator.Judge(ctx, Content{Kind: ContentKindProfile, Text: *v})
		if err != nil {
			slog.WarnContext(ctx, "moderation error; treating as ok", "err", err)
			continue
		}
		if verdict.Label == LabelBlock {
			return httpx.ErrModerationBlocked.With("categories", verdict.Categories)
		}
	}
	return nil
}

// atprotoSession はアプリセッションから PDS の ATProto セッション ID を取り出す。
func (s *MeService) atprotoSession(ctx context.Context, sid string) (string, error) {
	sess, err := model.GetAppSession(ctx, s.DB, sid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", httpx.ErrATProtoSession
	}
	if err != nil {
		return "", err
	}
	return sess.ATProtoSessionID, nil
}

// readProfileRecord は今の app.bsky.actor.profile/self を読む。無ければ $type だけの空 record を返す（cid は空）。
func (s *MeService) readProfileRecord(ctx context.Context, did, atsess string) (map[string]any, string, error) {
	if s.Writer == nil {
		return nil, "", httpx.ErrUpstream.With("reason", "PDS への書き込みができません")
	}
	record, cid, found, err := s.Writer.GetRecord(ctx, did, atsess, collectionBskyProfile, bskyProfileRKey)
	if err != nil {
		return nil, "", profileWriteError(err)
	}
	if !found || record == nil {
		return map[string]any{"$type": collectionBskyProfile}, "", nil
	}
	return record, cid, nil
}

func (s *MeService) writeProfileRecord(ctx context.Context, did, atsess string, record map[string]any, swapRecord string) (string, string, error) {
	uri, cid, err := s.Writer.PutRecord(ctx, did, atsess, collectionBskyProfile, bskyProfileRKey, record, swapRecord)
	if err != nil {
		return "", "", profileWriteError(err)
	}
	return uri, cid, nil
}

func (s *MeService) setProfileCID(ctx context.Context, did, field, cid string, now time.Time) error {
	if field == "avatar" {
		return model.SetProfileAvatar(ctx, s.DB, did, cid, now)
	}
	return model.SetProfileBanner(ctx, s.DB, did, cid, now)
}

// profileWriteError は Writer のエラーを API のエラーに写す（02 §3、13 §1.3、17 §2.4）。
func profileWriteError(err error) error {
	switch {
	case errors.Is(err, atproto.ErrSessionExpired):
		return httpx.ErrATProtoSession
	case errors.Is(err, atproto.ErrReauthRequired):
		return httpx.ErrReauthRequired
	case errors.Is(err, atproto.ErrSwapConflict):
		return httpx.ErrConflict
	default:
		return httpx.ErrUpstream.With("reason", "PDS への書き込みに失敗しました")
	}
}

func setOrDeleteString(record map[string]any, key, value string) {
	if value == "" {
		delete(record, key)
		return
	}
	record[key] = value
}

func stringField(record map[string]any, key string) string {
	if v, ok := record[key].(string); ok {
		return v
	}
	return ""
}
