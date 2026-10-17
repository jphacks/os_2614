package model

import (
	"database/sql"
	"time"

	"github.com/applemango/chain/internal/clock"
)

// このファイルは、複数の機能フォルダーから使う小さな共通関数と定数をまとめる。
// ルートのパッケージにだけ置き、機能フォルダーはここを import して使う。

// Space の種類（spaces.type）。グループと Party の判別に使う。
const (
	SpaceTypeGroup = "jp.i32.chain.space.group"
	SpaceTypeParty = "jp.i32.chain.space.party"
)

// Nullable は空文字を NULL にする。profiles などの任意の文字列列の保存に使う。
func Nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// NullableTime は nil を NULL に、非 nil を unix ミリ秒にする。
func NullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return clock.UnixMilli(*t)
}

// NullableString は nil の *string を NULL にする。空文字はそのまま入れる。
func NullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// Placeholders は IN (...) 用の "?,?,?" を作る。n が 0 以下なら空文字。
func Placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, n*2-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '?')
	}
	return string(out)
}

// Affected は sql.Result と error から更新行数を返す。err が非 nil ならそのまま返す。
func Affected(res sql.Result, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
