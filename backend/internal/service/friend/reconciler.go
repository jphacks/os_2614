package friend

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/model"
	friendmodel "github.com/applemango/chain/internal/model/friend"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/core"
)

// Reconciler は組ごとに「会ったフレンド」の条件（07 §2.4）を確かめ、friendships に入れる / 消す。
//
//  1. 相互フォロー（follows に両方向）で、どちらの向きにもブロックが無い
//  2. 会った裏付け: 招待コードを使った / revealed な encounter / 同じ Space のメンバーだった
//
// きっかけ: follows の増減、encounter の revealed / hidden、Space への参加、招待、ブロック。
type Reconciler struct {
	core.Deps
	Broker realtime.Broker
}

// 裏付けの経路。friendships.via の値と同じ（04 §2.3）。
const (
	ViaInvite    = "invite"
	ViaEncounter = "encounter"
	ViaSpace     = "space"
)

// reconcileInput は 1 組の判定に使う事実。DB から集めた結果を渡す（表駆動テストのため分けてある）。
type reconcileInput struct {
	Mutual    bool
	Blocked   bool
	Invite    bool
	Encounter bool
	Space     bool
	// Existing は今の friendships.via。行が無ければ空。
	Existing string
	// CapReached は新しく入れる場合に、どちらかが上限（MaxFriends）に達していること。
	CapReached bool
}

type reconcileAction int

const (
	actionNone reconcileAction = iota
	actionAdd
	actionRemove
)

// decide は入れる / 消す / 何もしないを決める。via は入れるときの経路。
func decide(in reconcileInput) (reconcileAction, string) {
	via := ""
	switch {
	case in.Invite:
		via = ViaInvite
	case in.Encounter:
		via = ViaEncounter
	case in.Space:
		via = ViaSpace
	}
	want := in.Mutual && !in.Blocked && via != ""
	switch {
	case in.Existing != "" && !want:
		return actionRemove, ""
	case in.Existing == "" && want && !in.CapReached:
		return actionAdd, via
	}
	return actionNone, ""
}

// ReconcileResult は 1 組を確かめた結果。Events は呼び出し側がコミット後に Publish する。
type ReconcileResult struct {
	Added   bool
	Removed bool
	Events  []realtime.Event
}

// Evidence は 2 人の間にある会った裏付けのうち最初のものの経路と参照（encounter / space の ID）を返す。無ければ空。
func (r *Reconciler) Evidence(ctx context.Context, q db.Querier, a, b string) (string, string, error) {
	if ok, err := friendmodel.HasInviteBetween(ctx, q, a, b); err != nil || ok {
		return ViaInvite, "", err
	}
	if id, ok, err := friendmodel.RevealedEncounterBetween(ctx, q, a, b); err != nil || ok {
		return ViaEncounter, id, err
	}
	if id, ok, err := friendmodel.SharedSpaceBetween(ctx, q, a, b); err != nil || ok {
		return ViaSpace, id, err
	}
	return "", "", nil
}

func (r *Reconciler) facts(ctx context.Context, q db.Querier, a, b string) (reconcileInput, error) {
	var in reconcileInput
	var err error
	if in.Mutual, err = friendmodel.IsMutualFollow(ctx, q, a, b); err != nil {
		return in, err
	}
	if in.Blocked, err = model.IsBlocked(ctx, q, a, b); err != nil {
		return in, err
	}
	if in.Invite, err = friendmodel.HasInviteBetween(ctx, q, a, b); err != nil {
		return in, err
	}
	if _, in.Encounter, err = friendmodel.RevealedEncounterBetween(ctx, q, a, b); err != nil {
		return in, err
	}
	if _, in.Space, err = friendmodel.SharedSpaceBetween(ctx, q, a, b); err != nil {
		return in, err
	}
	return in, nil
}

// usersActive は 2 人とも削除されていない利用者かを返す。
func usersActive(ctx context.Context, q db.Querier, dids ...string) (bool, error) {
	for _, did := range dids {
		u, err := model.GetUser(ctx, q, did)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if u.DeletedAt != nil {
			return false, nil
		}
	}
	return true, nil
}

// ReconcileTx は a, b の組を確かめ、friendships を入れる / 消す。q は呼び出し側のトランザクションでよい。
func (r *Reconciler) ReconcileTx(ctx context.Context, q db.Querier, a, b string) (*ReconcileResult, error) {
	a, b = core.OrderDIDs(a, b)
	res := &ReconcileResult{}
	if a == b {
		return res, nil
	}
	in, err := r.facts(ctx, q, a, b)
	if err != nil {
		return nil, err
	}
	if ok, err := usersActive(ctx, q, a, b); err != nil {
		return nil, err
	} else if !ok {
		in.Mutual = false
	}
	existing, err := friendmodel.GetFriendship(ctx, q, a, b)
	switch {
	case err == nil:
		in.Existing = existing.Via
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	if in.Existing == "" && in.Mutual && !in.Blocked {
		for _, did := range []string{a, b} {
			n, err := friendmodel.CountFriends(ctx, q, did)
			if err != nil {
				return nil, err
			}
			in.CapReached = in.CapReached || n >= MaxFriends
		}
	}

	action, via := decide(in)
	switch action {
	case actionAdd:
		if err := friendmodel.CreateFriendship(ctx, q, a, b, via, r.Clock.Now()); err != nil {
			return nil, err
		}
		res.Added = true
	case actionRemove:
		if _, err := friendmodel.DeleteFriendship(ctx, q, a, b); err != nil {
			return nil, err
		}
		if _, err := model.DeleteLocationSharesBetween(ctx, q, a, b); err != nil {
			return nil, err
		}
		res.Removed = true
	default:
		return res, nil
	}
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		peer, err := core.FetchUserSummary(ctx, q, pair[1])
		if err != nil {
			return nil, err
		}
		res.Events = append(res.Events, realtime.Event{Name: "friend.changed", User: pair[0], Data: peer})
	}
	return res, nil
}

// ReconcilePair は ReconcileTx を自分のトランザクションで行い、コミット後にイベントを配る。
func (r *Reconciler) ReconcilePair(ctx context.Context, a, b string) (*ReconcileResult, error) {
	var res *ReconcileResult
	err := db.Transaction(ctx, r.DB, func(tx *sql.Tx) error {
		var err error
		res, err = r.ReconcileTx(ctx, tx, a, b)
		return err
	})
	if err != nil {
		return nil, err
	}
	r.Publish(ctx, res.Events)
	return res, nil
}

// ReconcileUser は did と相互フォロー / 会ったフレンドの全員との組を確かめる。
func (r *Reconciler) ReconcileUser(ctx context.Context, did string) error {
	peers := map[string]bool{}
	mutual, err := friendmodel.ListMutualFollows(ctx, r.DB, did)
	if err != nil {
		return err
	}
	for _, m := range mutual {
		peers[m.DID] = true
	}
	friends, err := friendmodel.ListFriendships(ctx, r.DB, did)
	if err != nil {
		return err
	}
	for _, f := range friends {
		if f.UserA == did {
			peers[f.UserB] = true
		} else {
			peers[f.UserA] = true
		}
	}
	for peer := range peers {
		if _, err := r.ReconcilePair(ctx, did, peer); err != nil {
			return err
		}
	}
	return nil
}

// sweepLimit は 1 回の Sweep で確かめる組の上限。
const sweepLimit = 1000

// Sweep は相互フォローの組と friendships の組をまとめて確かめる。encounter の revealed / hidden や
// Space への参加のように、呼び出しを足していないきっかけの取りこぼしを拾う安全網（1 分ごと）。
func (r *Reconciler) Sweep(ctx context.Context) error {
	unmet, err := friendmodel.ListMutualFollowPairs(ctx, r.DB, true, sweepLimit)
	if err != nil {
		return err
	}
	met, err := friendmodel.ListAllFriendshipPairs(ctx, r.DB, sweepLimit)
	if err != nil {
		return err
	}
	for _, p := range append(unmet, met...) {
		if _, err := r.ReconcilePair(ctx, p[0], p[1]); err != nil {
			slog.WarnContext(ctx, "friend reconcile failed", "a", p[0], "b", p[1], "err", err)
		}
	}
	return nil
}

// Publish はコミット後のイベントを配る（10 §7.3）。
func (r *Reconciler) Publish(ctx context.Context, events []realtime.Event) {
	if r.Broker == nil {
		return
	}
	for _, e := range events {
		r.Broker.Publish(ctx, e)
	}
}
