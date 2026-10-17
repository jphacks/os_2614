package moderation

import "context"

// moderation の判定は 11 §5.5 の形。S5 では interface と、常に ok を返す Noop だけを置く。
// 実装（moderation.Jev）は S8 が差し込む。

type Label string

const (
	LabelOK    Label = "ok"
	LabelWarn  Label = "warn"
	LabelBlock Label = "block"
)

// Content は判定の対象。Kind は "instance_post" など（11 §5.5）。
type Content struct {
	Kind string
	Text string
}

type Verdict struct {
	Label      Label
	Categories []string
	Score      float64
}

// Moderator は投稿以外（グループ名など）の判定。
type Moderator interface {
	Judge(ctx context.Context, c Content) (Verdict, error)
}

// PostAssessor は instance の投稿を moderation と自動 Pin で 1 回に判定する（11 §5.5）。
type PostAssessor interface {
	Assess(ctx context.Context, c Content) (v Verdict, pin bool, err error)
}

// ModerationNoop は常に ok / pin なしを返す。CHAIN_NANOGPT_KEY が未設定のときの実装（11 §5.5）。
type ModerationNoop struct{}

var (
	_ Moderator    = ModerationNoop{}
	_ PostAssessor = ModerationNoop{}
)

func (ModerationNoop) Judge(context.Context, Content) (Verdict, error) {
	return Verdict{Label: LabelOK}, nil
}

func (ModerationNoop) Assess(context.Context, Content) (Verdict, bool, error) {
	return Verdict{Label: LabelOK}, false, nil
}
