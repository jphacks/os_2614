package model

import (
	"context"
	"database/sql"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

type Profile struct {
	DID         string
	DisplayName string
	Description string
	AvatarCID   string
	BannerCID   string
	Status      string
	UpdatedAt   time.Time
}

func GetProfile(ctx context.Context, q db.Querier, did string) (*Profile, error) {
	return db.GetItem(ctx, q, `
		SELECT did, display_name, description, avatar_cid, banner_cid, status, updated_at
		FROM profiles WHERE did = ?`,
		[]any{did}, func(scan db.Scan, p *Profile) error {
			var name, desc, avatar, banner, status sql.NullString
			var updated int64
			if err := scan(&p.DID, &name, &desc, &avatar, &banner, &status, &updated); err != nil {
				return err
			}
			p.DisplayName, p.Description = name.String, desc.String
			p.AvatarCID, p.BannerCID, p.Status = avatar.String, banner.String, status.String
			p.UpdatedAt = clock.FromUnixMilli(updated)
			return nil
		})
}

func UpsertProfile(ctx context.Context, q db.Querier, p Profile) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO profiles (did, display_name, description, avatar_cid, banner_cid, status, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (did) DO UPDATE SET
		  display_name = excluded.display_name, description = excluded.description,
		  avatar_cid = excluded.avatar_cid, banner_cid = excluded.banner_cid,
		  status = excluded.status, updated_at = excluded.updated_at`,
		p.DID, Nullable(p.DisplayName), Nullable(p.Description), Nullable(p.AvatarCID),
		Nullable(p.BannerCID), Nullable(p.Status), clock.UnixMilli(p.UpdatedAt))
	return err
}

// SetProfileStatus はステータスだけを更新する（13 §1.1）。空文字なら NULL に戻す。
func SetProfileStatus(ctx context.Context, q db.Querier, did, status string, now time.Time) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO profiles (did, status, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (did) DO UPDATE SET status = excluded.status, updated_at = excluded.updated_at`,
		did, Nullable(status), clock.UnixMilli(now))
	return err
}

// SetProfileText は displayName と description をまとめて更新する（13 §1.3）。空文字なら NULL に戻す。
// PDS の record を正として、record の最終状態をそのまま入れる。
func SetProfileText(ctx context.Context, q db.Querier, did, displayName, description string, now time.Time) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO profiles (did, display_name, description, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (did) DO UPDATE SET
		  display_name = excluded.display_name, description = excluded.description,
		  updated_at = excluded.updated_at`,
		did, Nullable(displayName), Nullable(description), clock.UnixMilli(now))
	return err
}

// SetProfileAvatar はアイコンの CID だけを更新する（13 §1.3）。空文字なら NULL に戻す。
func SetProfileAvatar(ctx context.Context, q db.Querier, did, cid string, now time.Time) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO profiles (did, avatar_cid, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (did) DO UPDATE SET avatar_cid = excluded.avatar_cid, updated_at = excluded.updated_at`,
		did, Nullable(cid), clock.UnixMilli(now))
	return err
}

// SetProfileBanner はバナーの CID だけを更新する（13 §1.3）。空文字なら NULL に戻す。
func SetProfileBanner(ctx context.Context, q db.Querier, did, cid string, now time.Time) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO profiles (did, banner_cid, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (did) DO UPDATE SET banner_cid = excluded.banner_cid, updated_at = excluded.updated_at`,
		did, Nullable(cid), clock.UnixMilli(now))
	return err
}
