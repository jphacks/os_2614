package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/auth"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
)

// ログインとアプリセッション（specification/backend/03-auth-atproto.md §2・§3）。

// OAuthClient は indigo の *oauth.ClientApp が満たす。テストでは fake に差し替える。
type OAuthClient interface {
	StartAuthFlow(ctx context.Context, identifier string) (string, error)
	ProcessCallback(ctx context.Context, params url.Values) (*oauth.ClientSessionData, error)
	Logout(ctx context.Context, did syntax.DID, sessionID string) error
}

type IdentityDirectory interface {
	LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error)
}

type ProfileFetcher interface {
	FetchProfile(ctx context.Context, pdsURL, did string) (*atproto.Profile, error)
}

const (
	refreshTTL = 30 * 24 * time.Hour
	// handle を省略したときの認可サーバ（03 §2）。
	defaultAuthServer = "https://bsky.social"
)

type AuthService struct {
	Deps
	OAuth    OAuthClient
	Dir      IdentityDirectory
	Profiles ProfileFetcher
	// FirstLoad は初回ログインの公開投稿の読み込み（17 §2.5）。nil なら何もしない。
	FirstLoad PublicPostBackfiller
}

type Tokens struct {
	DID             string    `json:"did"`
	AccessToken     string    `json:"accessToken"`
	AccessExpiresAt time.Time `json:"accessExpiresAt"`
	RefreshToken    string    `json:"refreshToken"`
}

// StartLogin は認可画面の URL を返す。handle が空なら bsky.social で handle を入力させる。
func (s *AuthService) StartLogin(ctx context.Context, handle string) (string, error) {
	identifier := strings.TrimPrefix(strings.TrimSpace(handle), "@")
	if identifier == "" {
		identifier = defaultAuthServer
	} else if _, err := syntax.ParseAtIdentifier(identifier); err != nil {
		return "", httpx.ErrInvalidRequest.With("fields", map[string]string{"handle": "handle か DID を指定してください"})
	}
	u, err := s.OAuth.StartAuthFlow(ctx, identifier)
	if err != nil {
		slog.WarnContext(ctx, "oauth start failed", "err", err)
		return "", httpx.ErrUpstream.With("reason", "ログインを開始できませんでした")
	}
	return u, nil
}

// CompleteLogin は OAuth のコールバックを処理し、アプリに渡す one-time code を返す。
func (s *AuthService) CompleteLogin(ctx context.Context, params url.Values) (string, error) {
	if e := params.Get("error"); e != "" {
		return "", httpx.ErrUnauthenticated.With("reason", e)
	}
	sess, err := s.OAuth.ProcessCallback(ctx, params)
	if err != nil {
		slog.WarnContext(ctx, "oauth callback failed", "err", err)
		return "", httpx.ErrUnauthenticated.With("reason", "ログインを完了できませんでした")
	}
	did := sess.AccountDID.String()

	handle := did
	if ident, err := s.Dir.LookupDID(ctx, sess.AccountDID); err == nil && !ident.Handle.IsInvalidHandle() {
		handle = ident.Handle.String()
	} else if err != nil {
		slog.WarnContext(ctx, "identity lookup failed", "did", did, "err", err)
	}

	now := s.Clock.Now()
	firstLogin := s.isFirstLogin(ctx, did)
	// users の行は Store.SaveSession が仮の handle で作っている。ここで正しい handle に直す。
	err = db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := model.UpsertUser(ctx, tx, did, handle, sess.HostURL, now); err != nil {
			return err
		}
		return model.EnsureSettings(ctx, tx, did, now)
	})
	if err != nil {
		return "", err
	}

	s.refreshProfile(ctx, did, sess.HostURL)
	if firstLogin {
		s.loadPublicPosts(did, sess.HostURL)
	}

	code := base64.RawURLEncoding.EncodeToString(s.IDs.Bytes(24))
	if err := model.SaveOneTimeCode(ctx, s.Redis, code, model.OneTimeCode{DID: did, ATProtoSessionID: sess.SessionID}); err != nil {
		return "", err
	}
	return code, nil
}

// isFirstLogin は users の行がこのログインで新しく作られたかを返す（17 §2.5）。
// Store.SaveSession が仮の handle（DID）で行を作るので、handle がまだ DID のままなら初回とみなす。
func (s *AuthService) isFirstLogin(ctx context.Context, did string) bool {
	u, err := model.GetUser(ctx, s.DB, did)
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	if err != nil {
		return false
	}
	return u.Handle == did
}

// loadPublicPosts は初回の読み込みを非同期で始める（17 §2.5）。ログインを遅らせないよう
// 呼び出し元の context から切り離し、タイムアウトを 30 秒にする。失敗はログだけにする。
func (s *AuthService) loadPublicPosts(did, pdsURL string) {
	if s.FirstLoad == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), FirstLoadTimeout)
		defer cancel()
		if err := s.FirstLoad.BackfillPublicPosts(ctx, did, pdsURL); err != nil {
			slog.WarnContext(ctx, "first public post load failed", "did", did, "err", err)
		}
	}()
}

// refreshProfile はプロフィールを取り直す。失敗してもログインは止めない。
func (s *AuthService) refreshProfile(ctx context.Context, did, pdsURL string) {
	p, err := s.Profiles.FetchProfile(ctx, pdsURL, did)
	if err != nil {
		slog.WarnContext(ctx, "profile fetch failed", "did", did, "err", err)
		return
	}
	err = model.UpsertProfile(ctx, s.DB, model.Profile{
		DID: did, DisplayName: p.DisplayName, Description: p.Description,
		AvatarCID: p.AvatarCID, BannerCID: p.BannerCID, Status: p.Status, UpdatedAt: s.Clock.Now(),
	})
	if err != nil {
		slog.WarnContext(ctx, "profile save failed", "did", did, "err", err)
	}
}

// Exchange は one-time code をアプリのトークンに換える（03 §2 の 7〜8）。
func (s *AuthService) Exchange(ctx context.Context, code, deviceName string) (*Tokens, error) {
	otc, err := model.TakeOneTimeCode(ctx, s.Redis, code)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrGone.With("reason", "コードが無効か期限切れです")
	}
	if err != nil {
		return nil, err
	}
	return s.issue(ctx, s.DB, otc.DID, otc.ATProtoSessionID, s.IDs.NewID(), deviceName)
}

// Refresh は refresh token をローテーションする。失効済みの token が来たら系列ごと失効させる。
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	hash := auth.GenerateSha256(refreshToken)
	now := s.Clock.Now()

	var tokens *Tokens
	var revoked []string
	reused := false
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		old, err := model.GetAppSessionByRefreshHash(ctx, tx, hash)
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.ErrUnauthenticated
		}
		if err != nil {
			return err
		}
		if old.RevokedAt != nil {
			// 再利用の検知: 漏洩とみなし、同じ系列をすべて失効させる（失効はコミットする）。
			slog.WarnContext(ctx, "refresh token reuse detected", "did", old.DID, "family", old.FamilyID)
			reused = true
			revoked, err = model.RevokeAppSessionFamily(ctx, tx, old.FamilyID, now)
			return err
		}
		if !old.ExpiresAt.After(now) {
			return httpx.ErrUnauthenticated
		}
		if _, err := model.RevokeAppSession(ctx, tx, old.ID, now); err != nil {
			return err
		}
		revoked = []string{old.ID}
		tokens, err = s.issue(ctx, tx, old.DID, old.ATProtoSessionID, old.FamilyID, old.DeviceName)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.uncache(ctx, revoked)
	if reused {
		return nil, httpx.ErrUnauthenticated
	}
	return tokens, nil
}

// Logout は自分のアプリセッションを失効させる。ATProto セッションを使うアプリセッションが
// 残っていなければ、ATProto 側のトークンも取り消す（03 §3）。
func (s *AuthService) Logout(ctx context.Context, did, sid string) error {
	now := s.Clock.Now()
	sess, err := model.GetAppSession(ctx, s.DB, sid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// 他人のセッションは失効させない（呼び出し元の DID と sid は同じ JWT から来るが、念のため確かめる）。
	if sess.DID != did {
		return httpx.ErrForbidden
	}
	if _, err := model.RevokeAppSession(ctx, s.DB, sid, now); err != nil {
		return err
	}
	s.uncache(ctx, []string{sid})

	n, err := model.CountActiveAppSessions(ctx, s.DB, sess.ATProtoSessionID, now)
	if err != nil {
		return err
	}
	if n == 0 {
		if err := s.OAuth.Logout(ctx, syntax.DID(sess.DID), sess.ATProtoSessionID); err != nil {
			slog.WarnContext(ctx, "atproto logout failed", "did", did, "err", err)
		}
	}
	return nil
}

func (s *AuthService) issue(ctx context.Context, q db.Querier, did, atprotoSessionID, familyID, deviceName string) (*Tokens, error) {
	now := s.Clock.Now()
	refresh := base64.RawURLEncoding.EncodeToString(s.IDs.Bytes(32))
	sess := model.AppSession{
		ID:               s.IDs.NewID(),
		DID:              did,
		ATProtoSessionID: atprotoSessionID,
		FamilyID:         familyID,
		RefreshHash:      auth.GenerateSha256(refresh),
		DeviceName:       deviceName,
		CreatedAt:        now,
		LastUsedAt:       now,
		ExpiresAt:        now.Add(refreshTTL),
	}
	if err := model.CreateAppSession(ctx, q, sess); err != nil {
		return nil, err
	}
	access, err := auth.IssueAccess(s.Config.JWTKey, did, sess.ID, now)
	if err != nil {
		return nil, err
	}
	return &Tokens{DID: did, AccessToken: access, AccessExpiresAt: now.Add(auth.AccessTTL), RefreshToken: refresh}, nil
}

func (s *AuthService) uncache(ctx context.Context, sids []string) {
	for _, sid := range sids {
		if err := model.SetSessionCache(ctx, s.Redis, sid, false); err != nil {
			slog.WarnContext(ctx, "session cache update failed", "sid", sid, "err", err)
		}
	}
}
