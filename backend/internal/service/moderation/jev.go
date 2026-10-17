package moderation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/applemango/chain/internal/external/nanogpt"
)

// ContentKindInstancePost は instance の投稿の判定種別（11 §5.5）。pin 質問を付ける唯一の種別。
const ContentKindInstancePost = "instance_post"

// ContentKindStatus はプロフィールのステータスの判定種別（11 §5.5、13 §1.1）。
const ContentKindStatus = "status"

// Jev は NanoGPT Decisions API を使う Moderator / PostAssessor の実装（11 §5）。
// 1 回の呼び出しで moderation（safety / category）と、instance の投稿では Pin も判定する。
type Jev struct {
	Client *nanogpt.Client
}

var (
	_ Moderator    = (*Jev)(nil)
	_ PostAssessor = (*Jev)(nil)
)

func (j *Jev) Judge(ctx context.Context, c Content) (Verdict, error) {
	resp, err := j.Client.Decide(ctx, j.request(c, false))
	if err != nil {
		return Verdict{}, err
	}
	v, _, err := interpretVerdict(resp)
	return v, err
}

func (j *Jev) Assess(ctx context.Context, c Content) (Verdict, bool, error) {
	resp, err := j.Client.Decide(ctx, j.request(c, c.Kind == ContentKindInstancePost))
	if err != nil {
		return Verdict{}, false, err
	}
	return interpretVerdict(resp)
}

// request は 11 §5.2 の本文を組み立てる。state には Kind と Text だけを入れ、
// DID・handle・Space 名は送らない（02 §8）。
func (j *Jev) request(c Content, withPin bool) nanogpt.Request {
	questions := map[string]nanogpt.Question{
		"safety":   {Type: "choice", Instructions: promptSafetyInstructions, Criteria: safetyCriteria()},
		"category": {Type: "choice", Instructions: promptCategoryInstructions, Criteria: categoryCriteria()},
	}
	if withPin {
		questions["pin"] = nanogpt.Question{Type: "noul", Instructions: promptPinInstructions}
	}
	return nanogpt.Request{
		State:     nanogpt.State{Kind: c.Kind, Text: c.Text},
		Questions: questions,
	}
}

// interpretVerdict は 11 §5.3 のしきい値を適用する。
func interpretVerdict(resp *nanogpt.Response) (Verdict, bool, error) {
	safety, ok := resp.Answers["safety"]
	if !ok {
		return Verdict{}, false, fmt.Errorf("nanogpt: safety の回答がありません")
	}
	pBlock := safety.Probabilities["block"]
	pWarn := safety.Probabilities["warn"]
	score := pBlock + pWarn

	label := LabelOK
	switch {
	case pBlock >= BlockThreshold:
		label = LabelBlock
	case score >= WarnThreshold:
		label = LabelWarn
	}

	categories := []string{}
	if cat, ok := resp.Answers["category"]; ok {
		for name, p := range cat.Probabilities {
			if name == "none" || p < CategoryThreshold {
				continue
			}
			categories = append(categories, name)
		}
		sort.Strings(categories)
	}

	pin := false
	if p, ok := resp.Answers["pin"]; ok {
		pin = p.Noul >= PinThreshold
	}
	return Verdict{Label: label, Categories: categories, Score: score}, pin, nil
}

// LogModerationError は 11 §5.4 の扱い。400（質問・モデルの指定誤り）と 401（鍵の誤り）は
// こちらの不具合なのでエラーログを出し、それ以外（429・5xx・タイムアウト）は警告に留める。
// いずれも呼び出し側は pending として保存し、再判定する。本文・鍵はログに出さない（02 §7）。
func LogModerationError(ctx context.Context, err error) {
	var apiErr *nanogpt.APIError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == 400 || apiErr.StatusCode == 401) {
		slog.ErrorContext(ctx, "moderation api error", "status", apiErr.StatusCode)
		return
	}
	slog.WarnContext(ctx, "moderation error; storing pending", "err", err)
}
