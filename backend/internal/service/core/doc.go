// Package core は service 層の共通部分を持つ。
//
// 機能フォルダーとルートの service の両方から使う共通の依存（Deps）・パラメータ（params.go）・
// geohash・画像処理（image.go）・共通ヘルパー（common.go）を、この core に置く。
// core はどの機能フォルダーからも import できるいちばん下のパッケージで、
// ほかの service パッケージを import しない。
package core

import (
	"database/sql"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/config"
	"github.com/applemango/chain/internal/idgen"
)

// Deps は全 service に共通の依存。
type Deps struct {
	Config *config.Config
	DB     *sql.DB
	Redis  *redis.Client
	Clock  clock.Clock
	IDs    idgen.Generator
}
