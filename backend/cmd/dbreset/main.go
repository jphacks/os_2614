// dbreset は DB を作り直す（specification/backend/04-data-model.md §5）。
//
//	go run ./cmd/dbreset          スキーマと seed
//	go run ./cmd/dbreset -no-seed スキーマだけ
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/config"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/idgen"
	"github.com/applemango/chain/internal/kv"
	core "github.com/applemango/chain/internal/service/core"
	groupsvc "github.com/applemango/chain/internal/service/group"
)

func main() {
	noSeed := flag.Bool("no-seed", false, "seed を入れない")
	flag.Parse()
	if err := run(*noSeed); err != nil {
		fmt.Fprintln(os.Stderr, "dbreset:", err)
		os.Exit(1)
	}
}

func run(noSeed bool) error {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !cfg.IsDev() {
		return errors.New("refusing to reset outside CHAIN_ENV=dev")
	}

	if err := db.RemoveFiles(cfg.DBPath); err != nil {
		return err
	}
	conn, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := db.ApplySchema(ctx, conn); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	if !noSeed {
		if err := db.ApplySeed(ctx, conn); err != nil {
			return fmt.Errorf("seed: %w", err)
		}
	}

	// 揮発データが SQLite と食い違わないように Redis も空にする。
	rdb, err := kv.Open(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer rdb.Close()
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		return err
	}

	// seed のグループを geo:groups に載せ直す（09 §5）。
	deps := core.Deps{Config: cfg, DB: conn, Redis: rdb, Clock: clock.Real{}, IDs: idgen.Real{}}
	if err := (&groupsvc.GroupService{Deps: deps}).RebuildGeoIndex(ctx); err != nil {
		return fmt.Errorf("geo index: %w", err)
	}

	fmt.Printf("reset %s (seed=%v) and flushed redis\n", cfg.DBPath, !noSeed)
	return nil
}
