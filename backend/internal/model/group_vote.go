package model

import (
	"context"
	"time"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
)

// GroupVote は group_votes の行（04 §2.4、09 §8.6）。投票の種類と期限を持つ。
type GroupVote struct {
	ID        string
	SpaceID   string
	Kind      string
	CreatedBy string
	Status    string
	CreatedAt time.Time
	ClosesAt  time.Time
}

const groupVoteColumns = `id, space_id, kind, created_by, status, created_at, closes_at`

func scanGroupVote(scan db.Scan, v *GroupVote) error {
	var created, closes int64
	if err := scan(&v.ID, &v.SpaceID, &v.Kind, &v.CreatedBy, &v.Status, &created, &closes); err != nil {
		return err
	}
	v.CreatedAt = clock.FromUnixMilli(created)
	v.ClosesAt = clock.FromUnixMilli(closes)
	return nil
}

func GetGroupVote(ctx context.Context, q db.Querier, id string) (*GroupVote, error) {
	return db.GetItem(ctx, q,
		"SELECT "+groupVoteColumns+" FROM group_votes WHERE id = ?",
		[]any{id}, scanGroupVote)
}

// GetOpenGroupVote は同じ種類で開いている投票を返す（09 §8.6 の二重発議の判定）。
func GetOpenGroupVote(ctx context.Context, q db.Querier, spaceID, kind string) (*GroupVote, error) {
	return db.GetItem(ctx, q,
		"SELECT "+groupVoteColumns+" FROM group_votes WHERE space_id = ? AND kind = ? AND status = 'open'",
		[]any{spaceID, kind}, scanGroupVote)
}

func InsertGroupVote(ctx context.Context, q db.Querier, v GroupVote) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO group_votes (id, space_id, kind, created_by, status, created_at, closes_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.SpaceID, v.Kind, v.CreatedBy, v.Status,
		clock.UnixMilli(v.CreatedAt), clock.UnixMilli(v.ClosesAt))
	return err
}

// ListGroupVotes は開いている投票と、since 以降に作られた投票を新しい順で返す（09 §8.6 の一覧）。
func ListGroupVotes(ctx context.Context, q db.Querier, spaceID string, since time.Time) ([]GroupVote, error) {
	return db.GetItems(ctx, q,
		"SELECT "+groupVoteColumns+` FROM group_votes
		 WHERE space_id = ? AND (status = 'open' OR created_at >= ?)
		 ORDER BY created_at DESC, id DESC`,
		[]any{spaceID, clock.UnixMilli(since)}, scanGroupVote)
}

// ListDueGroupVotes は期限を過ぎても開いている投票を古い順で返す（締め切りのワーカー用）。
func ListDueGroupVotes(ctx context.Context, q db.Querier, now time.Time, limit int) ([]GroupVote, error) {
	return db.GetItems(ctx, q,
		"SELECT "+groupVoteColumns+` FROM group_votes
		 WHERE status = 'open' AND closes_at <= ?
		 ORDER BY closes_at LIMIT ?`,
		[]any{clock.UnixMilli(now), limit}, scanGroupVote)
}

// CloseGroupVote は開いている投票だけを passed / failed にする。既に締まっていれば false を返す。
func CloseGroupVote(ctx context.Context, q db.Querier, id, status string) (bool, error) {
	res, err := db.Execute(ctx, q,
		"UPDATE group_votes SET status = ? WHERE id = ? AND status = 'open'", status, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// UpsertGroupVoteBallot は票を入れ直す（同じ人の 2 回目は上書き。09 §8.6）。
func UpsertGroupVoteBallot(ctx context.Context, q db.Querier, voteID, did string, yes bool) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO group_vote_ballots (vote_id, did, yes) VALUES (?, ?, ?)
		ON CONFLICT (vote_id, did) DO UPDATE SET yes = excluded.yes`,
		voteID, did, yes)
	return err
}

// GetGroupVoteBallot はその人の票を返す。行がなければ sql.ErrNoRows をそのまま返す。
func GetGroupVoteBallot(ctx context.Context, q db.Querier, voteID, did string) (*bool, error) {
	var yes int
	if err := q.QueryRowContext(ctx,
		"SELECT yes FROM group_vote_ballots WHERE vote_id = ? AND did = ?", voteID, did).Scan(&yes); err != nil {
		return nil, err
	}
	b := yes != 0
	return &b, nil
}

// CountGroupVoteBallots は賛成・反対の数を返す。
func CountGroupVoteBallots(ctx context.Context, q db.Querier, voteID string) (int, int, error) {
	var yes, no int
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(yes), 0), COALESCE(SUM(1 - yes), 0)
		FROM group_vote_ballots WHERE vote_id = ?`, voteID).Scan(&yes, &no)
	return yes, no, err
}
