package space

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/idgen"
	"github.com/applemango/chain/internal/utils"
)

// SpaceRef は Spaces の space-ref（03 §7.1、10 §2）。
type SpaceRef struct {
	Authority string
	Type      string
	ID        string
}

// String は at://{authority}/space/{type}/{skey} を返す。
func (r SpaceRef) String() string {
	return "at://" + r.Authority + "/space/" + r.Type + "/" + r.ID
}

// RecordURI は at://{authority}/space/{type}/{skey}/{author}/{collection}/{rkey} を返す（10 §2）。
func (r SpaceRef) RecordURI(author string, collection string, rkey string) string {
	return r.String() + "/" + author + "/" + collection + "/" + rkey
}

// SpaceRef は Space 行から space-ref を作る。
func (s Space) SpaceRef() SpaceRef {
	return SpaceRef{Authority: s.Authority, Type: s.Type, ID: s.ID}
}

// InstanceStore は Space 内の record の読み書き（03 §7.2）。
// P0 は SQLiteStore だけを実装し、Spaces 統合時に差し替える。
type InstanceStore interface {
	CreateRecord(ctx context.Context, space SpaceRef, author syntax.DID, collection syntax.NSID, value any) (Record, error)
	DeleteRecord(ctx context.Context, uri syntax.ATURI) error
	GetRecord(ctx context.Context, uri syntax.ATURI) (Record, error)
	ListRecords(ctx context.Context, space SpaceRef, collection syntax.NSID, cursor string, limit int) ([]Record, string, error)
}

// SQLiteStore は InstanceStore の SQLite 実装。rkey は TID（10 §2）を使う。
type SQLiteStore struct {
	DB    *sql.DB
	Clock clock.Clock
	TID   idgen.TIDGenerator
}

func NewSQLiteStore(db *sql.DB, clk clock.Clock, tid idgen.TIDGenerator) *SQLiteStore {
	return &SQLiteStore{DB: db, Clock: clk, TID: tid}
}

var _ InstanceStore = (*SQLiteStore)(nil)

func (s *SQLiteStore) CreateRecord(ctx context.Context, space SpaceRef, author syntax.DID, collection syntax.NSID, value any) (Record, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return Record{}, fmt.Errorf("marshal record value: %w", err)
	}
	rkey := s.TID.Next().String()
	r := Record{
		URI:        space.RecordURI(author.String(), collection.String(), rkey),
		SpaceID:    space.ID,
		AuthorDID:  author.String(),
		Collection: collection.String(),
		RKey:       rkey,
		ValueJSON:  string(b),
		CreatedAt:  s.Clock.Now(),
		ModLabel:   "ok",
		ModState:   "done",
	}
	if err := InsertRecord(ctx, s.DB, r); err != nil {
		return Record{}, err
	}
	return r, nil
}

func (s *SQLiteStore) DeleteRecord(ctx context.Context, uri syntax.ATURI) error {
	_, err := SoftDeleteRecord(ctx, s.DB, uri.String(), "", s.Clock.Now())
	return err
}

func (s *SQLiteStore) GetRecord(ctx context.Context, uri syntax.ATURI) (Record, error) {
	r, err := GetRecord(ctx, s.DB, uri.String())
	if err != nil {
		return Record{}, err
	}
	return *r, nil
}

func (s *SQLiteStore) ListRecords(ctx context.Context, space SpaceRef, collection syntax.NSID, cursor string, limit int) ([]Record, string, error) {
	beforeAt, beforeURI, err := utils.DecodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	records, err := ListRecordsInSpace(ctx, s.DB, space.ID, []string{collection.String()}, beforeAt, beforeURI, limit)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(records) == limit && limit > 0 {
		last := records[len(records)-1]
		next = utils.EncodeCursor(last.CreatedAt.UnixMilli(), last.URI)
	}
	return records, next, nil
}
