package proximity

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"

	"github.com/applemango/chain/internal/auth"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service/core"
)

// BeaconService は beacon token の発行（05 §2）。
type BeaconService struct {
	core.Deps
}

type BeaconToken struct {
	WindowStart time.Time
	Token       string // base64url(16 bytes)
}

// Tokens は現在の窓から hours 時間分の token を返す。発行済みの窓は同じ token を返す（冪等）。
func (s *BeaconService) Tokens(ctx context.Context, did string, hours int) (int, []BeaconToken, error) {
	if hours < 1 || hours > core.BeaconIssueHoursMax {
		return 0, nil, httpx.ErrInvalidRequest.With("fields", map[string]string{
			"hours": "1 から 24 の範囲で指定してください",
		})
	}
	now := s.Clock.Now()
	start := core.WindowStart(now)
	count := hours * int(time.Hour/core.BeaconWindow)

	out := make([]BeaconToken, 0, count)
	for i := 0; i < count; i++ {
		ws := start + int64(i*core.BeaconWindowSec)
		tok, err := s.ensureToken(ctx, did, ws, now)
		if err != nil {
			return 0, nil, err
		}
		out = append(out, tok)
	}
	return core.BeaconWindowSec, out, nil
}

func (s *BeaconService) ensureToken(ctx context.Context, did string, windowStartSec int64, now time.Time) (BeaconToken, error) {
	row, err := model.GetBeaconTokenByWindow(ctx, s.DB, did, windowStartSec)
	switch {
	case err == nil:
		return s.decodeToken(row)
	case !errors.Is(err, sql.ErrNoRows):
		return BeaconToken{}, err
	}

	plain := s.IDs.Bytes(16)
	hash := auth.Sha256(plain)
	enc, err := auth.Encrypt(s.Config.SecretKey, plain)
	if err != nil {
		return BeaconToken{}, err
	}
	row = &model.BeaconToken{TokenHash: hash, TokenEnc: enc, DID: did, WindowStart: windowStartSec}
	if err := model.CreateBeaconToken(ctx, s.DB, *row); err != nil {
		// 競合したら、先に入った行を読み直して同じ token を返す（冪等）。
		again, gerr := model.GetBeaconTokenByWindow(ctx, s.DB, did, windowStartSec)
		if gerr != nil {
			return BeaconToken{}, err
		}
		return s.decodeToken(again)
	}
	s.cacheLookup(ctx, hash, did, windowStartSec, now)
	return BeaconToken{
		WindowStart: time.Unix(windowStartSec, 0).UTC(),
		Token:       base64.RawURLEncoding.EncodeToString(plain),
	}, nil
}

func (s *BeaconService) decodeToken(row *model.BeaconToken) (BeaconToken, error) {
	plain, err := auth.Decrypt(s.Config.SecretKey, row.TokenEnc)
	if err != nil {
		return BeaconToken{}, err
	}
	return BeaconToken{
		WindowStart: time.Unix(row.WindowStart, 0).UTC(),
		Token:       base64.RawURLEncoding.EncodeToString(plain),
	}, nil
}

func (s *BeaconService) cacheLookup(ctx context.Context, hash []byte, did string, windowStartSec int64, now time.Time) {
	expiry := time.Unix(windowStartSec+int64(core.BeaconWindowSec), 0).Add(time.Hour)
	ttl := expiry.Sub(now)
	if ttl <= 0 {
		return
	}
	_ = model.SetBeaconLookup(ctx, s.Redis, auth.Hex(hash), did, windowStartSec, ttl)
}

// ResolveBeaconToken は token（base64url）を発行元 DID と窓に解決し、観測時刻が窓の許容幅に入るかを確かめる（05 §2.4）。
func ResolveBeaconToken(ctx context.Context, deps core.Deps, token string, first, last time.Time) (did string, ok bool, err error) {
	plain, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(plain) != 16 {
		return "", false, nil
	}
	hash := auth.Sha256(plain)

	var windowStartSec int64
	var found bool
	if did, windowStartSec, found, err = model.GetBeaconLookup(ctx, deps.Redis, auth.Hex(hash)); err != nil {
		return "", false, err
	}
	if !found {
		row, gerr := model.GetBeaconTokenByHash(ctx, deps.DB, hash)
		if errors.Is(gerr, sql.ErrNoRows) {
			return "", false, nil
		}
		if gerr != nil {
			return "", false, gerr
		}
		did, windowStartSec = row.DID, row.WindowStart
	}
	if !ObservationInWindow(windowStartSec, first, last) {
		return "", false, nil
	}
	return did, true, nil
}

// ObservationInWindow は観測区間が [window_start - skew, window_start + window + skew] に収まるかを返す（05 §2.4）。
func ObservationInWindow(windowStartSec int64, first, last time.Time) bool {
	lo := windowStartSec - int64(core.BeaconClockSkew/time.Second)
	hi := windowStartSec + int64(core.BeaconWindowSec) + int64(core.BeaconClockSkew/time.Second)
	return first.Unix() >= lo && last.Unix() <= hi
}
