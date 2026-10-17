// Package model はデータの型と、SQLite / Redis への読み書きを持つ。
//
// 規則:
//   - 1 つの機能につき 1 ファイル（user.go、encounter.go、space.go …）。
//   - 型は DB の行に対応させる。時刻は time.Time で持ち、保存時に unix ミリ秒へ変換する。
//   - 関数は第 1 引数に context、第 2 引数に db.Querier（*sql.DB / *sql.Tx）か *redis.Client を取る。
//     トランザクションの境界は service が決める。
//   - 業務ルール（権限、状態遷移、しきい値）は書かない。それは service の責務。
//   - 行がなければ sql.ErrNoRows をそのまま返す。
package model
