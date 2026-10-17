// Package service は業務ロジックを持つ。
//
// 規則:
//   - 1 つの機能につき 1 ファイル・1 つの型（EncounterService など）。
//   - 依存は Deps と、外部サービス（internal/external/*）の interface で受け取る。
//     時刻は Deps.Clock、ID と乱数は Deps.IDs から取る（time.Now を直接呼ばない）。
//   - トランザクションの境界はここで決める（db.Transaction）。
//   - API に返すエラーは httpx.Err*（必要なら .With で details を足す）。それ以外のエラーは 500 になる。
//   - Fiber の型（fiber.Ctx）に依存しない。controller から呼ばれる純粋な Go の関数にする。
package service

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
