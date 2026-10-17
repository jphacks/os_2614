package atproto

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/auth"
	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/model"
)

// Store は indigo の oauth.ClientAuthStore を実装する（03 §2）。
// 認可要求は Redis に 10 分、セッションは SQLite に保存し、どちらも CHAIN_SECRET_KEY で暗号化する。
type Store struct {
	DB        *sql.DB
	Redis     *redis.Client
	SecretKey []byte
	Clock     clock.Clock
}

const authRequestTTL = 10 * time.Minute

var _ oauth.ClientAuthStore = (*Store)(nil)

func (s *Store) GetSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSessionData, error) {
	row, err := model.GetATProtoSession(ctx, s.DB, sessionID)
	if err != nil {
		return nil, err
	}
	if row.DID != did.String() || row.RevokedAt != nil {
		return nil, sql.ErrNoRows
	}
	var data oauth.ClientSessionData
	if err := s.open(row.DataEnc, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// SaveSession は upsert。ログイン完了時（ProcessCallback の中）とトークン更新時に indigo から呼ばれる。
// 初回は users の行がまだないので、外部キーのために仮の handle（DID）で作る。正しい handle は service が入れる。
func (s *Store) SaveSession(ctx context.Context, sess oauth.ClientSessionData) error {
	enc, err := s.seal(sess)
	if err != nil {
		return err
	}
	did, now := sess.AccountDID.String(), s.Clock.Now()
	return db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := model.CreateUserIfMissing(ctx, tx, did, sess.HostURL, now); err != nil {
			return err
		}
		return model.SaveATProtoSession(ctx, tx, sess.SessionID, did, enc, now)
	})
}

func (s *Store) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
	row, err := model.GetATProtoSession(ctx, s.DB, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.DID != did.String() {
		return sql.ErrNoRows
	}
	return model.RevokeATProtoSession(ctx, s.DB, sessionID, s.Clock.Now())
}

func (s *Store) GetAuthRequestInfo(ctx context.Context, state string) (*oauth.AuthRequestData, error) {
	b, err := model.GetOAuthRequest(ctx, s.Redis, state)
	if err != nil {
		return nil, err
	}
	var info oauth.AuthRequestData
	if err := s.open(b, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (s *Store) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
	enc, err := s.seal(info)
	if err != nil {
		return err
	}
	ok, err := model.CreateOAuthRequest(ctx, s.Redis, info.State, enc, authRequestTTL)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("oauth auth request already exists")
	}
	return nil
}

func (s *Store) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	return model.DeleteOAuthRequest(ctx, s.Redis, state)
}

func (s *Store) seal(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return auth.Encrypt(s.SecretKey, b)
}

func (s *Store) open(sealed []byte, v any) error {
	b, err := auth.Decrypt(s.SecretKey, sealed)
	if err != nil {
		return fmt.Errorf("decrypt oauth data: %w", err)
	}
	return json.Unmarshal(b, v)
}
