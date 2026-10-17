package moderation

import "time"

// moderation のしきい値とプロンプト（11 §5.2、§5.3）。文言を変えたら PromptVersion を上げる。
const (
	// PromptVersion は Jev に送る文言の版（11 §5.2）。判定結果を比較するときに使う。
	PromptVersion = 1

	// BlockThreshold は block と判定する p(block) の下限（11 §5.3）。
	BlockThreshold = 0.70
	// WarnThreshold は warn と判定する p(block)+p(warn) の下限（11 §5.3）。
	WarnThreshold = 0.60
	// PinThreshold は Pin する pin.noul の下限（11 §5.3、10 §6）。
	PinThreshold = 0.75
	// CategoryThreshold は Verdict.Categories に載せる category 確率の下限（11 §5.3）。
	CategoryThreshold = 0.30

	// ModerationSyncTimeout は同期判定のタイムアウト（11 §5.1）。
	ModerationSyncTimeout = 2 * time.Second
	// ModerationRequeueTimeout は再判定ワーカーのタイムアウト（11 §5.1）。
	ModerationRequeueTimeout = 10 * time.Second
)

// 11 §5.2 の instr句。model は CHAIN_JEV_MODEL（既定 typesafe/jev-1.13）。
const (
	promptSafetyInstructions = "SNS の投稿として、この文章をどう扱うべきか。日本語の文脈とスラングを考慮すること。"
	promptSafetyOK           = "問題のない内容"
	promptSafetyWarn         = "攻撃的・性的・暴力的・センシティブだが、特定の個人や集団への加害ではない内容"
	promptSafetyBlock        = "特定の個人や集団への誹謗中傷・脅迫・差別、個人情報の暴露、自傷や犯罪の扇動"

	promptCategoryInstructions = "最も当てはまる問題の種類"
	promptPinInstructions      = "グループのメンバーが後で見返す必要のある情報（日時、場所、持ち物、締切、連絡事項）を含むか"
)

// categoryNames は category 質問の選択肢（none を除く分類）。11 §5.2 の並び。
var categoryNames = []string{
	"none", "harassment", "hate", "sexual", "violence", "self_harm", "personal_info", "spam",
}

// safetyCriteria は safety 質問の criteria。ok / warn / block に説明を付ける。
func safetyCriteria() map[string]*string {
	ok, warn, block := promptSafetyOK, promptSafetyWarn, promptSafetyBlock
	return map[string]*string{"ok": &ok, "warn": &warn, "block": &block}
}

// categoryCriteria は category 質問の criteria。説明は付けず、値はすべて null（11 §5.2）。
func categoryCriteria() map[string]*string {
	c := make(map[string]*string, len(categoryNames))
	for _, name := range categoryNames {
		c[name] = nil
	}
	return c
}
