package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/realtime"
)

// GroupVoteService はグループの投票（09 §8.6）。
// 未退出のメンバーが発議・投票し、賛成がその時点のメンバー数の過半数に達したら可決する。
type GroupVoteService struct {
	Deps
	Access *AccessService
	Broker realtime.Broker
}

// VoteItem は投票 1 件の詳細。一覧と発議・投票の応答に使う。
type VoteItem struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Status        string    `json:"status"`
	CreatedBy     string    `json:"createdBy"`
	YesCount      int       `json:"yesCount"`
	NoCount       int       `json:"noCount"`
	RequiredCount int       `json:"requiredCount"`
	CreatedAt     time.Time `json:"createdAt"`
	ClosesAt      time.Time `json:"closesAt"`
	MyBallot      *bool     `json:"myBallot"`
}

// VoteList は GET /groups/{id}/votes の応答（09 §8.6）。
type VoteList struct {
	Items []VoteItem `json:"items"`
}

// groupVotePassedPayload は group.vote_passed の payload（12 §3、09 §8.6）。遷移先は Space。
type groupVotePassedPayload struct {
	Text    string `json:"text"`
	SpaceID string `json:"spaceId"`
	VoteID  string `json:"voteId"`
	Kind    string `json:"kind"`
}

// voteClose は 1 回の締め切りで起きたこと。通知はコミット後に配信する。
type voteClose struct {
	Passed bool
	Notes  []notificationToSend
}

// Propose は 09 §8.6。未退出のメンバーなら誰でも発議できる。発議者は自動で賛成票を入れる。
func (s *GroupVoteService) Propose(ctx context.Context, did, spaceID, kind string) (*VoteItem, error) {
	if err := validateVoteKind(kind); err != nil {
		return nil, err
	}
	sp, group, err := s.loadGroup(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	if err := s.requireMember(ctx, spaceID, did); err != nil {
		return nil, err
	}
	if sp.State == "archived" {
		return nil, httpx.ErrGone
	}
	if err := s.checkTarget(group, kind); err != nil {
		return nil, err
	}
	if _, err := model.GetOpenGroupVote(ctx, s.DB, spaceID, kind); err == nil {
		return nil, httpx.ErrConflict.With("fields", map[string]string{"kind": "同じ種類の投票が開いています"})
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	now := s.Clock.Now()
	vote := model.GroupVote{
		ID: s.IDs.NewID(), SpaceID: spaceID, Kind: kind, CreatedBy: did,
		Status: "open", CreatedAt: now, ClosesAt: now.Add(GroupVoteDuration),
	}
	var closed *voteClose
	if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		if err := model.InsertGroupVote(ctx, tx, vote); err != nil {
			return err
		}
		if err := model.UpsertGroupVoteBallot(ctx, tx, vote.ID, did, true); err != nil {
			return err
		}
		var err error
		closed, err = s.tallyAndClose(ctx, tx, vote, now, true)
		return err
	}); err != nil {
		return nil, err
	}

	item, err := s.detail(ctx, vote.ID, did)
	if err != nil {
		return nil, err
	}
	s.publish(ctx, "group.vote_opened", spaceID, item)
	s.afterClose(ctx, spaceID, closed, item)
	return item, nil
}

// CastBallot は 09 §8.6。投票し直せる。開いていない投票への票は 410。
func (s *GroupVoteService) CastBallot(ctx context.Context, did, spaceID, voteID string, yes bool) (*VoteItem, error) {
	sp, _, err := s.loadGroup(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	if err := s.requireMember(ctx, spaceID, did); err != nil {
		return nil, err
	}
	if sp.State == "archived" {
		return nil, httpx.ErrGone
	}
	vote, err := s.voteInGroup(ctx, spaceID, voteID)
	if err != nil {
		return nil, err
	}
	if vote.Status != "open" {
		return nil, httpx.ErrGone
	}

	now := s.Clock.Now()
	expired := !now.Before(vote.ClosesAt)
	var closed *voteClose
	if err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
		var err error
		if expired {
			closed, err = s.tallyAndClose(ctx, tx, *vote, now, false)
			return err
		}
		if err := model.UpsertGroupVoteBallot(ctx, tx, vote.ID, did, yes); err != nil {
			return err
		}
		closed, err = s.tallyAndClose(ctx, tx, *vote, now, true)
		return err
	}); err != nil {
		return nil, err
	}

	if expired {
		if closed != nil {
			item, derr := s.detail(ctx, voteID, did)
			if derr != nil {
				return nil, derr
			}
			s.afterClose(ctx, spaceID, closed, item)
		}
		return nil, httpx.ErrGone
	}

	item, err := s.detail(ctx, voteID, did)
	if err != nil {
		return nil, err
	}
	s.afterClose(ctx, spaceID, closed, item)
	return item, nil
}

// List は 09 §8.6。メンバーに、開いている投票と最近（7 日以内）に作られた投票を返す。
func (s *GroupVoteService) List(ctx context.Context, did, spaceID string) (*VoteList, error) {
	if _, _, err := s.loadGroup(ctx, spaceID); err != nil {
		return nil, err
	}
	if err := s.requireMember(ctx, spaceID, did); err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	votes, err := model.ListGroupVotes(ctx, s.DB, spaceID, now.Add(-GroupVoteListWindow))
	if err != nil {
		return nil, err
	}
	items := make([]VoteItem, 0, len(votes))
	for _, v := range votes {
		item, err := s.voteItem(ctx, s.DB, v, did)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return &VoteList{Items: items}, nil
}

// CloseDue は期限を過ぎた投票を締める（09 §8.6 の締め切り処理）。締めた件数を返す。
func (s *GroupVoteService) CloseDue(ctx context.Context) (int, error) {
	now := s.Clock.Now()
	due, err := model.ListDueGroupVotes(ctx, s.DB, now, 100)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, v := range due {
		var res *voteClose
		err := db.Transaction(ctx, s.DB, func(tx *sql.Tx) error {
			cur, err := model.GetGroupVote(ctx, tx, v.ID)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if cur.Status != "open" || now.Before(cur.ClosesAt) {
				return nil
			}
			res, err = s.tallyAndClose(ctx, tx, *cur, now, false)
			return err
		})
		if err != nil {
			return closed, err
		}
		if res == nil {
			continue
		}
		closed++
		item, err := s.detail(ctx, v.ID, "")
		if err != nil {
			return closed, err
		}
		s.afterClose(ctx, v.SpaceID, res, item)
	}
	return closed, nil
}

// tallyAndClose は票を数えて、可決なら group を変えて通知を積む。
// onlyIfPassed が true のときは、可決に達していなければ何もしない（開いたまま）。
func (s *GroupVoteService) tallyAndClose(ctx context.Context, tx *sql.Tx, vote model.GroupVote, now time.Time, onlyIfPassed bool) (*voteClose, error) {
	yes, _, err := model.CountGroupVoteBallots(ctx, tx, vote.ID)
	if err != nil {
		return nil, err
	}
	members, err := model.ListActiveSpaceMembers(ctx, tx, vote.SpaceID)
	if err != nil {
		return nil, err
	}
	passed := yes*2 > len(members)
	if onlyIfPassed && !passed {
		return nil, nil
	}

	status := "failed"
	if passed {
		status = "passed"
	}
	changed, err := model.CloseGroupVote(ctx, tx, vote.ID, status)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, nil
	}
	if !passed {
		return &voteClose{}, nil
	}

	switch vote.Kind {
	case "make_closed":
		if err := model.SetGroupVisibility(ctx, tx, vote.SpaceID, "closed"); err != nil {
			return nil, err
		}
	case "make_persistent":
		if err := model.SetGroupPersistent(ctx, tx, vote.SpaceID, true); err != nil {
			return nil, err
		}
	}

	name := ""
	sp, err := model.GetSpace(ctx, tx, vote.SpaceID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if sp != nil {
		name = sp.Name
	}
	res := &voteClose{Passed: true}
	payload := groupVotePassedPayload{
		Text: votePassedText(name, vote.Kind), SpaceID: vote.SpaceID, VoteID: vote.ID, Kind: vote.Kind,
	}
	for _, m := range members {
		item, err := enqueueNotification(ctx, tx, s.Deps, m.DID, NotificationGroupVotePassed, payload, now)
		if err != nil {
			return nil, err
		}
		if item != nil {
			res.Notes = append(res.Notes, notificationToSend{DID: m.DID, Item: *item})
		}
	}
	return res, nil
}

// afterClose は締め切り後の後始末。可決でグループが変わったので判定キャッシュを消し、イベントを配信する。
func (s *GroupVoteService) afterClose(ctx context.Context, spaceID string, closed *voteClose, item *VoteItem) {
	if closed == nil {
		return
	}
	if closed.Passed {
		s.invalidateSpace(ctx, spaceID)
	}
	s.publish(ctx, "group.vote_closed", spaceID, item)
	for _, n := range closed.Notes {
		s.publishEvent(ctx, realtime.Event{Name: "notification", User: n.DID, Data: n.Item})
	}
}

func (s *GroupVoteService) detail(ctx context.Context, voteID, viewer string) (*VoteItem, error) {
	v, err := model.GetGroupVote(ctx, s.DB, voteID)
	if err != nil {
		return nil, err
	}
	return s.voteItem(ctx, s.DB, *v, viewer)
}

func (s *GroupVoteService) voteItem(ctx context.Context, q db.Querier, v model.GroupVote, viewer string) (*VoteItem, error) {
	yes, no, err := model.CountGroupVoteBallots(ctx, q, v.ID)
	if err != nil {
		return nil, err
	}
	members, err := model.CountActiveMembers(ctx, q, v.SpaceID)
	if err != nil {
		return nil, err
	}
	var ballot *bool
	if viewer != "" {
		ballot, err = model.GetGroupVoteBallot(ctx, q, v.ID, viewer)
		if errors.Is(err, sql.ErrNoRows) {
			ballot = nil
		} else if err != nil {
			return nil, err
		}
	}
	return &VoteItem{
		ID: v.ID, Kind: v.Kind, Status: v.Status, CreatedBy: v.CreatedBy,
		YesCount: yes, NoCount: no, RequiredCount: members/2 + 1,
		CreatedAt: v.CreatedAt, ClosesAt: v.ClosesAt, MyBallot: ballot,
	}, nil
}

func (s *GroupVoteService) loadGroup(ctx context.Context, id string) (*model.Space, *model.Group, error) {
	sp, err := model.GetSpace(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if sp.State == "deleted" || sp.Type != spaceTypeGroup {
		return nil, nil, httpx.ErrNotFound
	}
	group, err := model.GetGroup(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return sp, group, nil
}

func (s *GroupVoteService) voteInGroup(ctx context.Context, spaceID, voteID string) (*model.GroupVote, error) {
	v, err := model.GetGroupVote(ctx, s.DB, voteID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if v.SpaceID != spaceID {
		return nil, httpx.ErrNotFound
	}
	return v, nil
}

func (s *GroupVoteService) requireMember(ctx context.Context, spaceID, did string) error {
	m, err := model.GetSpaceMember(ctx, s.DB, spaceID, did)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrForbidden
	}
	if err != nil {
		return err
	}
	if m.LeftAt != nil {
		return httpx.ErrForbidden
	}
	return nil
}

func (s *GroupVoteService) checkTarget(group *model.Group, kind string) error {
	if kind == "make_closed" && group.Visibility == "closed" {
		return httpx.ErrConflict.With("fields", map[string]string{"kind": "既に closed です"})
	}
	if kind == "make_persistent" && group.Persistent {
		return httpx.ErrConflict.With("fields", map[string]string{"kind": "既に persistent です"})
	}
	return nil
}

func (s *GroupVoteService) invalidateSpace(ctx context.Context, spaceID string) {
	if s.Access != nil {
		_ = s.Access.InvalidateSpace(ctx, spaceID)
	}
}

func (s *GroupVoteService) publish(ctx context.Context, name, spaceID string, item *VoteItem) {
	s.publishEvent(ctx, realtime.Event{Name: name, Space: spaceID, Data: item})
}

func (s *GroupVoteService) publishEvent(ctx context.Context, e realtime.Event) {
	if s.Broker != nil {
		s.Broker.Publish(ctx, e)
	}
}

func validateVoteKind(kind string) error {
	if kind != "make_closed" && kind != "make_persistent" {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{
			"kind": "make_closed か make_persistent を指定してください",
		})
	}
	return nil
}

func votePassedText(groupName, kind string) string {
	switch kind {
	case "make_closed":
		return "『" + groupName + "』が招待制のグループになりました"
	case "make_persistent":
		return "『" + groupName + "』が、エリア外でも使えるグループになりました"
	default:
		return "『" + groupName + "』の投票が可決しました"
	}
}
