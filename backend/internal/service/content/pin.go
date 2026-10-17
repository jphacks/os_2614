package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/httpx"
	spacemodel "github.com/applemango/chain/internal/model/space"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/access"
	"github.com/applemango/chain/internal/service/core"
)

// Pin（10 §6）。自動判定（Assess）と手動 API の両方から作られ、authority の名義で
// jp.i32.chain.instance.pin を records に書く。
const (
	// MaxPinsPerInstance は 1 instance につき有効な Pin の上限（10 §6）。
	MaxPinsPerInstance = 10

	PinReasonAuto   = "auto"
	PinReasonManual = "manual"
)

// Pin は API が返す Pin 1 件（10 §7.3 の pin.created の data）。
type Pin struct {
	URI       string    `json:"uri"`
	Subject   string    `json:"subject"`
	Reason    string    `json:"reason"`
	PinnedBy  string    `json:"pinnedBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// pinValue は jp.i32.chain.instance.pin の record の中身（10 §3）。
type pinValue struct {
	Subject   string `json:"subject"`
	Reason    string `json:"reason"`
	PinnedBy  string `json:"pinnedBy,omitempty"`
	CreatedAt string `json:"createdAt"`
}

type PinService struct {
	core.Deps
	Store  spacemodel.InstanceStore
	Access access.AccessChecker
	Broker realtime.Broker
}

// List は Space の有効な Pin を新しい順で返す（10 §6）。read 権限が要る。
func (s *PinService) List(ctx context.Context, did, spaceID string) ([]Pin, error) {
	ok, err := s.Access.Check(ctx, spaceID, syntax.DID(did), access.ModeRead)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.ErrForbidden
	}
	records, err := spacemodel.ListActivePins(ctx, s.DB, spaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Pin, 0, len(records))
	for i := len(records) - 1; i >= 0; i-- {
		out = append(out, pinFrom(&records[i]))
	}
	return out, nil
}

// CreateManual は手動 Pin（10 §6）。write 権限が要り、subject は同じ Space の有効な投稿。
func (s *PinService) CreateManual(ctx context.Context, did, spaceID, subject string) (*Pin, error) {
	ok, err := s.Access.Check(ctx, spaceID, syntax.DID(did), access.ModeWrite)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.ErrForbidden
	}
	sp, err := spacemodel.GetSpace(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.validateSubject(ctx, spaceID, subject); err != nil {
		return nil, err
	}
	pinned, err := s.pinnedSubject(ctx, spaceID, subject)
	if err != nil {
		return nil, err
	}
	if pinned {
		return nil, httpx.ErrConflict.With("reason", "既に Pin されています")
	}
	if err := s.makeRoom(ctx, spaceID); err != nil {
		return nil, err
	}
	return s.create(ctx, sp, PinReasonManual, did, subject, s.Clock.Now())
}

// CreateAuto は自動 Pin（10 §6）。投稿作成と再判定ワーカーから呼ばれる。
// 既に Pin 済み、または上限に空きがなければ何もしない（投稿自体は成功させる）。
func (s *PinService) CreateAuto(ctx context.Context, spaceID, subject string) error {
	sp, err := spacemodel.GetSpace(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	pinned, err := s.pinnedSubject(ctx, spaceID, subject)
	if err != nil {
		return err
	}
	if pinned {
		return nil
	}
	if err := s.makeRoom(ctx, spaceID); err != nil {
		return err
	}
	_, err = s.create(ctx, sp, PinReasonAuto, "", subject, s.Clock.Now())
	return err
}

// Delete は手動 Pin を外す（10 §6）。write 権限が要る。
func (s *PinService) Delete(ctx context.Context, did, spaceID, rkey string) error {
	ok, err := s.Access.Check(ctx, spaceID, syntax.DID(did), access.ModeWrite)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.ErrForbidden
	}
	rec, err := spacemodel.GetActivePin(ctx, s.DB, spaceID, rkey)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.remove(ctx, rec)
}

// DeleteBySubject は Pin されていた投稿が削除されたときに、その Pin を消す（10 §6）。
func (s *PinService) DeleteBySubject(ctx context.Context, spaceID, subject string) error {
	records, err := spacemodel.ListActivePins(ctx, s.DB, spaceID)
	if err != nil {
		return err
	}
	for i := range records {
		if decodePin(&records[i]).Subject == subject {
			if err := s.remove(ctx, &records[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *PinService) validateSubject(ctx context.Context, spaceID, subject string) error {
	invalid := httpx.ErrInvalidRequest.With("fields", map[string]string{"subject": "同じ Space の投稿を指定してください"})
	rec, err := spacemodel.GetRecord(ctx, s.DB, subject)
	if errors.Is(err, sql.ErrNoRows) {
		return invalid
	}
	if err != nil {
		return err
	}
	if rec.SpaceID != spaceID || rec.Collection != core.CollectionPost || rec.DeletedAt != nil {
		return invalid
	}
	return nil
}

func (s *PinService) pinnedSubject(ctx context.Context, spaceID, subject string) (bool, error) {
	records, err := spacemodel.ListActivePins(ctx, s.DB, spaceID)
	if err != nil {
		return false, err
	}
	for i := range records {
		if decodePin(&records[i]).Subject == subject {
			return true, nil
		}
	}
	return false, nil
}

// makeRoom は上限に達していれば最も古い auto の Pin を外す。auto が無ければ 409（10 §6）。
func (s *PinService) makeRoom(ctx context.Context, spaceID string) error {
	records, err := spacemodel.ListActivePins(ctx, s.DB, spaceID)
	if err != nil {
		return err
	}
	if len(records) < MaxPinsPerInstance {
		return nil
	}
	for i := range records {
		if decodePin(&records[i]).Reason == PinReasonAuto {
			return s.remove(ctx, &records[i])
		}
	}
	return httpx.ErrConflict.With("reason", "Pin の上限に達しています")
}

func (s *PinService) create(ctx context.Context, sp *spacemodel.Space, reason, pinnedBy, subject string, now time.Time) (*Pin, error) {
	value := pinValue{Subject: subject, Reason: reason, PinnedBy: pinnedBy, CreatedAt: core.FormatDateTime(now)}
	rec, err := s.Store.CreateRecord(ctx, sp.SpaceRef(), syntax.DID(sp.Authority), syntax.NSID(core.CollectionPin), value)
	if err != nil {
		return nil, err
	}
	pin := pinFrom(&rec)
	s.publish(ctx, realtime.Event{Name: "pin.created", Space: sp.ID, Data: pin})
	return &pin, nil
}

func (s *PinService) remove(ctx context.Context, rec *spacemodel.Record) error {
	if _, err := spacemodel.SoftDeleteRecord(ctx, s.DB, rec.URI, "", s.Clock.Now()); err != nil {
		return err
	}
	s.publish(ctx, realtime.Event{Name: "pin.deleted", Space: rec.SpaceID, Data: map[string]string{"uri": rec.URI}})
	return nil
}

func (s *PinService) publish(ctx context.Context, e realtime.Event) {
	if s.Broker != nil {
		s.Broker.Publish(ctx, e)
	}
}

func pinFrom(rec *spacemodel.Record) Pin {
	v := decodePin(rec)
	return Pin{URI: rec.URI, Subject: v.Subject, Reason: v.Reason, PinnedBy: v.PinnedBy, CreatedAt: rec.CreatedAt}
}

func decodePin(rec *spacemodel.Record) pinValue {
	var v pinValue
	_ = json.Unmarshal([]byte(rec.ValueJSON), &v)
	return v
}
