// Package model はデータの型と、SQLite / Redis への読み書きを持つ。
//
// 規則:
//   - 型は DB の行に対応させる。時刻は time.Time で持ち、保存時に unix ミリ秒へ変換する。
//   - 関数は第 1 引数に context、第 2 引数に db.Querier（*sql.DB / *sql.Tx）か *redis.Client を取る。
//     トランザクションの境界は service が決める。
//   - 業務ルール（権限、状態遷移、しきい値）は書かない。それは service の責務。
//   - 行がなければ sql.ErrNoRows をそのまま返す。
//
// フォルダーの分け方:
//   - 複数のファイルを持つ機能は、機能ごとのフォルダー（＝ Go のパッケージ）に分ける。
//     機能フォルダーは encounter / friend / space / group / schedule / maintenance。
//   - 機能フォルダーはレベルの順にしか import しない（循環を作らない）:
//     model ← encounter, friend, space, group, schedule ← maintenance。
//   - ルートの model には、共通部分（helpers.go）と 1 ファイルだけの機能だけを置く
//     （audit, beacon, block, device, jetstream, location, lock, modq, notification, oauth,
//     party, profile, proximity, public_post, relationship, session, settings, stats, user,
//     user_handle）。
//   - ファイル名からは機能の接頭辞を落とす（group_invite.go → group/invite.go）。
//   - フォルダーをまたいで使う名前は公開する（先頭を大文字）。ほかのフォルダーの
//     scan 関数や列名の定数も、必要なら公開して共有する。
//
// import の別名:
//   - 同じ層のルートはそのまま model、機能フォルダーは本来の名前（space など）。
//   - 別の層（service など）から機能フォルダーを使うときは <名前>model
//     （例 groupmodel "github.com/applemango/chain/internal/model/group"）。
package model
