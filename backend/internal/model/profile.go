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
		p.DID, nullable(p.DisplayName), nullable(p.Description), nullable(p.AvatarCID),
		nullable(p.BannerCID), nullable(p.Status), clock.UnixMilli(p.UpdatedAt))
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
