package utils

import (
	"regexp"
	"unicode"
)

// GraphemeCount は grapheme クラスタ数を近似する（17 §2.1 の maxGraphemes 1000）。
// rivo/uniseg を使わない方針のため、結合文字（Mn / Me）・書式文字（Cf。ZWJ を含む）・
// 肌色の絵文字修飾子（Sk）は前の文字に付くものとして数えない。地域表示記号の連続
// （国旗）は 1 文字ずつ数えるなど、絵文字の連結は近似の限界がある。
func GraphemeCount(s string) int {
	n := 0
	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf):
		case r >= 0x1F3FB && r <= 0x1F3FF:
		default:
			n++
		}
	}
	return n
}

// languageTag は BCP-47 風の言語タグ（"ja"、"en-US"、"zh-Hant" など）。完全な検証はしない。
var languageTag = regexp.MustCompile(`^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$`)

// ValidLanguageTag は langs に受け付ける BCP-47 風の文字列かを返す（17 §2.1）。
func ValidLanguageTag(s string) bool { return languageTag.MatchString(s) }
