package friend

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	friendmodel "github.com/applemango/chain/internal/model/friend"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/notify"
)

// Bluesky のフォロー・ブロックの取り込みと書き込み（07 §2.2・§2.5）。

// GraphResyncInterval は ログイン時に follow / block を読み直す間隔（07 §2.5）。
const GraphResyncInterval = 24 * time.Hour

// followEffects はフォローを索引に入れた・消した結果、コミット後に配るもの。
type followEffects struct {
	events []realtime.Event
	notes  []notify.NotificationToSend
	// mutual は索引に入れたフォローで相互フォローになったこと。
	mutual bool
}

// reconcileInto は組を確かめ、イベントを effects に足す。
func (s *FriendService) reconcileInto(ctx context.Context, q db.Querier, a, b string, effects *followEffects) (*ReconcileResult, error) {
	res, err := s.reconciler().ReconcileTx(ctx, q, a, b)
	if err != nil {
		return nil, err
	}
	effects.events = append(effects.events, res.Events...)
	return res, nil
}

// indexFollow は author → subject のフォローを follows に入れ、組を確かめる。既にあれば何もしない。
// notify が真なら、受信箱に出るフォローには friend.request を、相互フォローになったときには friend.accepted を送る。
func (s *FriendService) indexFollow(ctx context.Context, q db.Querier, author, subject, uri string, createdAt time.Time, effects *followEffects) (*friendmodel.Follow, error) {
	return s.indexFollowN(ctx, q, author, subject, uri, createdAt, true, effects)
}

func (s *FriendService) indexFollowN(ctx context.Context, q db.Querier, author, subject, uri string, createdAt time.Time, notifyPeer bool, effects *followEffects) (*friendmodel.Follow, error) {
	now := s.Clock.Now()
	if createdAt.IsZero() {
		createdAt = now
	}
	f := friendmodel.Follow{
		ID: s.IDs.NewID(), URI: uri, Author: author, Subject: subject, CreatedAt: createdAt, IndexedAt: now,
	}
	created, err := friendmodel.InsertFollow(ctx, q, f)
	if err != nil {
		return nil, err
	}
	if !created {
		existing, err := friendmodel.GetFollow(ctx, q, author, subject)
		if err != nil {
			return nil, err
		}
		return existing, nil
	}

	mutual, err := friendmodel.IsMutualFollow(ctx, q, author, subject)
	if err != nil {
		return nil, err
	}
	effects.mutual = effects.mutual || mutual
	if _, err := s.reconcileInto(ctx, q, author, subject, effects); err != nil {
		return nil, err
	}
	if !notifyPeer {
		return &f, nil
	}
	active, err := usersActive(ctx, q, author, subject)
	if err != nil || !active {
		return &f, err
	}
	authorSummary, err := core.FetchUserSummary(ctx, q, author)
	if err != nil {
		return nil, err
	}
	if mutual {
		// 先にフォローした側（subject）に、承認されたことを知らせる。
		if err := s.notify(ctx, q, subject, notify.NotificationFriendAccepted, friendNotificationPayload{
			Text: friendAcceptedText(*authorSummary), Actor: *authorSummary,
		}, now, &effects.notes); err != nil {
			return nil, err
		}
		effects.events = append(effects.events,
			realtime.Event{Name: notify.NotificationFriendAccepted, User: subject, Data: authorSummary})
		return &f, nil
	}
	// 受信箱に出るフォロー（§2 の関係があり、拒否されておらず、ブロックが無い）だけを知らせる。
	dismissed, err := friendmodel.IsDismissed(ctx, q, subject, author)
	if err != nil {
		return nil, err
	}
	blocked, err := model.IsBlocked(ctx, q, author, subject)
	if err != nil {
		return nil, err
	}
	via, _, err := s.reconciler().Evidence(ctx, q, author, subject)
	if err != nil {
		return nil, err
	}
	if dismissed || blocked || via == "" {
		return &f, nil
	}
	if err := s.notify(ctx, q, subject, notify.NotificationFriendRequest, friendNotificationPayload{
		Text: friendRequestText(*authorSummary), Actor: *authorSummary, RequestID: f.ID,
	}, now, &effects.notes); err != nil {
		return nil, err
	}
	subjectSummary, err := core.FetchUserSummary(ctx, q, subject)
	if err != nil {
		return nil, err
	}
	effects.events = append(effects.events,
		realtime.Event{Name: notify.NotificationFriendRequest, User: subject, Data: authorSummary},
		realtime.Event{Name: "friend.changed", User: author, Data: subjectSummary},
	)
	return &f, nil
}

// removeFollow は author → subject のフォローの索引を消し、組を確かめ直す。
func (s *FriendService) removeFollow(ctx context.Context, q db.Querier, author, subject string, effects *followEffects) error {
	if err := friendmodel.DeleteFollow(ctx, q, author, subject); err != nil {
		return err
	}
	// 付け直したフォローを、前の拒否が隠さないようにする。
	if err := friendmodel.DeleteDismissal(ctx, q, subject, author); err != nil {
		return err
	}
	_, err := s.reconcileInto(ctx, q, author, subject, effects)
	return err
}

// atprotoSession はアプリセッションから PDS の ATProto セッション ID を取り出す。
func (s *FriendService) atprotoSession(ctx context.Context, sid string) (string, error) {
	sess, err := model.GetAppSession(ctx, s.DB, sid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", httpx.ErrATProtoSession
	}
	if err != nil {
		return "", err
	}
	return sess.ATProtoSessionID, nil
}

// followAs は did のセッションで subject へのフォローを書き、record の URI を返す。
// 既にフォローしていれば何も書かず空を返す。
func (s *FriendService) followAs(ctx context.Context, did, sid, subject string) (string, error) {
	if has, err := friendmodel.HasFollow(ctx, s.DB, did, subject); err != nil {
		return "", err
	} else if has {
		return "", nil
	}
	atsess, err := s.atprotoSession(ctx, sid)
	if err != nil {
		return "", err
	}
	uri, err := s.writeFollow(ctx, did, atsess, subject)
	if err != nil {
		return "", graphWriteError(err)
	}
	return uri, nil
}

// writeFollow は app.bsky.graph.follow を書く。エラーは atproto のもののまま返す。
func (s *FriendService) writeFollow(ctx context.Context, did, atsess, subject string) (string, error) {
	if s.Writer == nil {
		return "", errors.New("record writer is not configured")
	}
	record := map[string]any{
		"$type":     atproto.CollectionBskyFollow,
		"subject":   subject,
		"createdAt": core.FormatDateTime(s.Clock.Now()),
	}
	uri, _, err := s.Writer.CreateRecord(ctx, did, atsess, atproto.CollectionBskyFollow, record)
	return uri, err
}

// deleteFollowRecords は did の subject へのフォローを、PDS で見つかった分すべて消す（07 §5）。
func (s *FriendService) deleteFollowRecords(ctx context.Context, did, atsess, subject string) error {
	if s.Writer == nil {
		return httpx.ErrUpstream.With("reason", "PDS への書き込みができません")
	}
	uris := map[string]bool{}
	if f, err := friendmodel.GetFollow(ctx, s.DB, did, subject); err == nil {
		uris[f.URI] = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if s.Lister != nil {
		if u, err := model.GetUser(ctx, s.DB, did); err == nil {
			recs, err := s.Lister.ListRecords(ctx, u.PDSURL, did, atproto.CollectionBskyFollow)
			if err != nil {
				slog.WarnContext(ctx, "follow: listRecords failed; deleting the indexed record only", "did", did, "err", err)
			}
			for _, r := range recs {
				if g, err := atproto.ParseGraphRecord(r.Value); err == nil && g.Subject == subject {
					uris[r.URI] = true
				}
			}
		}
	}
	for uri := range uris {
		rkey := atproto.RKeyFromURI(uri)
		if rkey == "" {
			continue
		}
		err := s.Writer.DeleteRecord(ctx, did, atsess, atproto.CollectionBskyFollow, rkey)
		if err != nil && !errors.Is(err, atproto.ErrRecordNotFound) {
			return graphWriteError(err)
		}
	}
	return nil
}

// graphWriteError は Writer のエラーを API のエラーに写す（03 §4、17 §2.4）。
func graphWriteError(err error) error {
	var he *httpx.Error
	switch {
	case errors.As(err, &he):
		return err
	case errors.Is(err, atproto.ErrSessionExpired):
		return httpx.ErrATProtoSession
	case errors.Is(err, atproto.ErrReauthRequired):
		return httpx.ErrReauthRequired
	default:
		return httpx.ErrUpstream.With("reason", "PDS への書き込みに失敗しました")
	}
}

// HandleGraphCommit は Jetstream の app.bsky.graph.follow / block のイベントを取り込む（07 §2.5）。
// 作者が users にいないイベントは呼び出し側（JetstreamService.Handle）が捨てている。
func (s *FriendService) HandleGraphCommit(ctx context.Context, did string, c *atproto.JetstreamCommit) error {
	if c == nil || c.RKey == "" {
		return nil
	}
	uri := atproto.GraphURI(did, c.Collection, c.RKey)
	var effects followEffects
	err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		switch c.Collection {
		case atproto.CollectionBskyFollow:
			return s.applyFollowCommit(ctx, tx, did, uri, c, &effects)
		case atproto.CollectionBskyBlock:
			return s.applyBlockCommit(ctx, tx, did, uri, c, &effects)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, effects.events, effects.notes)
	return nil
}

func (s *FriendService) applyFollowCommit(ctx context.Context, tx *sql.Tx, did, uri string, c *atproto.JetstreamCommit, effects *followEffects) error {
	switch c.Operation {
	case "create", "update":
		rec, err := atproto.ParseGraphRecord(c.Record)
		if err != nil || rec.Subject == did {
			return nil
		}
		if old, err := friendmodel.GetFollowByURI(ctx, tx, uri); err == nil && old.Subject != rec.Subject {
			if err := s.removeFollow(ctx, tx, old.Author, old.Subject, effects); err != nil {
				return err
			}
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = s.indexFollow(ctx, tx, did, rec.Subject, uri, rec.CreatedAt, effects)
		return err
	case "delete":
		old, err := friendmodel.GetFollowByURI(ctx, tx, uri)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return s.removeFollow(ctx, tx, old.Author, old.Subject, effects)
	}
	return nil
}

func (s *FriendService) applyBlockCommit(ctx context.Context, tx *sql.Tx, did, uri string, c *atproto.JetstreamCommit, effects *followEffects) error {
	switch c.Operation {
	case "create", "update":
		rec, err := atproto.ParseGraphRecord(c.Record)
		if err != nil || rec.Subject == did {
			return nil
		}
		// blocks.blocked は users を参照するので、CHAIN の利用者へのブロックだけを持つ。
		if ok, err := usersActive(ctx, tx, rec.Subject); err != nil || !ok {
			return err
		}
		if err := friendmodel.UpsertBskyBlock(ctx, tx, did, rec.Subject, uri, s.Clock.Now()); err != nil {
			return err
		}
		_, err = s.reconcileInto(ctx, tx, did, rec.Subject, effects)
		return err
	case "delete":
		old, err := friendmodel.GetBskyBlockByURI(ctx, tx, uri)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := friendmodel.DeleteBskyBlock(ctx, tx, old.Blocker, old.Blocked); err != nil {
			return err
		}
		_, err = s.reconcileInto(ctx, tx, old.Blocker, old.Blocked, effects)
		return err
	}
	return nil
}

// SyncOnLogin はログイン時の仕事をまとめて行う（07 §2.1・§2.5）。
//   - 招待で書けなかった作成者のフォロー（follow_pending）を、このセッションで書く
//   - 初回と、前回から 24 時間以上たっていれば、PDS の follow と block を読み直す
//
// 失敗してもログインは止めない（呼び出し側が非同期で呼び、結果はログにだけ出す）。
func (s *FriendService) SyncOnLogin(ctx context.Context, did, pdsURL, atprotoSessionID string) error {
	var firstErr error
	if err := s.writePendingInviteFollows(ctx, did, atprotoSessionID); err != nil {
		firstErr = err
	}
	if s.Lister == nil {
		return firstErr
	}
	now := s.Clock.Now()
	last, err := friendmodel.UserGraphSyncedAt(ctx, s.DB, did)
	if err != nil {
		return err
	}
	if last != nil && now.Sub(*last) < GraphResyncInterval {
		return firstErr
	}
	if err := s.resyncFollows(ctx, did, pdsURL); err != nil {
		return err
	}
	if err := s.resyncBlocks(ctx, did, pdsURL); err != nil {
		return err
	}
	if err := friendmodel.SetUserGraphSyncedAt(ctx, s.DB, did, now); err != nil {
		return err
	}
	return firstErr
}

func (s *FriendService) writePendingInviteFollows(ctx context.Context, did, atsess string) error {
	pending, err := friendmodel.ListPendingInviteFollows(ctx, s.DB, did)
	if err != nil {
		return err
	}
	var firstErr error
	for _, inv := range pending {
		var uri string
		if has, err := friendmodel.HasFollow(ctx, s.DB, did, inv.UsedBy); err != nil {
			return err
		} else if !has {
			uri, err = s.writeFollow(ctx, did, atsess, inv.UsedBy)
			if err != nil {
				slog.WarnContext(ctx, "invite: pending follow still not written", "did", did, "err", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		var effects followEffects
		err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
			if uri != "" {
				if _, err := s.indexFollowN(ctx, tx, did, inv.UsedBy, uri, time.Time{}, false, &effects); err != nil {
					return err
				}
			}
			return friendmodel.SetFriendInviteFollowPending(ctx, tx, inv.CodeHash, false)
		})
		if err != nil {
			return err
		}
		s.publish(ctx, effects.events, effects.notes)
	}
	return firstErr
}

// resyncFollows は PDS の follow を読み直す。読み直した結果に無いフォローは消す（07 §2.5）。
func (s *FriendService) resyncFollows(ctx context.Context, did, pdsURL string) error {
	recs, err := s.Lister.ListRecords(ctx, pdsURL, did, atproto.CollectionBskyFollow)
	if err != nil {
		return err
	}
	type listed struct {
		uri     string
		created time.Time
	}
	// 同じ相手へのフォローが複数あれば、古い方を持つ。
	bySubject := map[string]listed{}
	liveURIs := map[string]bool{}
	for _, r := range recs {
		g, err := atproto.ParseGraphRecord(r.Value)
		if err != nil || g.Subject == did {
			continue
		}
		liveURIs[r.URI] = true
		cur, ok := bySubject[g.Subject]
		if !ok || (!g.CreatedAt.IsZero() && g.CreatedAt.Before(cur.created)) {
			bySubject[g.Subject] = listed{uri: r.URI, created: g.CreatedAt}
		}
	}

	var effects followEffects
	err = db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		existing, err := friendmodel.ListFollowsByAuthor(ctx, tx, did)
		if err != nil {
			return err
		}
		have := map[string]bool{}
		for _, f := range existing {
			if _, ok := bySubject[f.Subject]; !ok || !liveURIs[f.URI] {
				if err := s.removeFollow(ctx, tx, f.Author, f.Subject, &effects); err != nil {
					return err
				}
				continue
			}
			have[f.Subject] = true
		}
		for subject, l := range bySubject {
			if have[subject] {
				continue
			}
			if _, err := s.indexFollowN(ctx, tx, did, subject, l.uri, l.created, false, &effects); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, effects.events, effects.notes)
	return nil
}

// resyncBlocks は PDS の block を読み直し、blocks の source = 'bsky' に反映する。
func (s *FriendService) resyncBlocks(ctx context.Context, did, pdsURL string) error {
	recs, err := s.Lister.ListRecords(ctx, pdsURL, did, atproto.CollectionBskyBlock)
	if err != nil {
		return err
	}
	live := map[string]string{} // subject → uri
	for _, r := range recs {
		g, err := atproto.ParseGraphRecord(r.Value)
		if err != nil || g.Subject == did {
			continue
		}
		live[g.Subject] = r.URI
	}
	var effects followEffects
	err = db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		existing, err := friendmodel.ListBskyBlocks(ctx, tx, did)
		if err != nil {
			return err
		}
		for _, b := range existing {
			if _, ok := live[b.Blocked]; ok {
				continue
			}
			if err := friendmodel.DeleteBskyBlock(ctx, tx, did, b.Blocked); err != nil {
				return err
			}
			if _, err := s.reconcileInto(ctx, tx, did, b.Blocked, &effects); err != nil {
				return err
			}
		}
		for subject, uri := range live {
			if ok, err := usersActive(ctx, tx, subject); err != nil {
				return err
			} else if !ok {
				continue
			}
			if err := friendmodel.UpsertBskyBlock(ctx, tx, did, subject, uri, s.Clock.Now()); err != nil {
				return err
			}
			if _, err := s.reconcileInto(ctx, tx, did, subject, &effects); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, effects.events, effects.notes)
	return nil
}
