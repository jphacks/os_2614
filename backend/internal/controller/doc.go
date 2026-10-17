// Package controller は Fiber のハンドラを持つ。
//
// 規則:
//   - 1 つの機能につき 1 ファイル・1 つの型（EncounterController など）。
//     型は自分が使う service だけをフィールドに持ち、Register でルートを登録する。
//   - やることは「入力を読む（httpx.Bind）→ service を呼ぶ → レスポンスを返す」だけ。業務ロジックを書かない。
//   - リクエスト / レスポンスの JSON の型はこのパッケージに置き、model の型をそのまま返さない
//     （DB の列名や内部の値を API に漏らさないため）。
//   - エラーは service から返ったものをそのまま return する。形式は httpx.ErrorHandler が整える。
//
// フォルダーの分け方:
//   - 複数のファイルを持つ機能は、機能ごとのフォルダー（＝ Go のパッケージ）に分ける。
//     機能フォルダーは auth / encounter / friend / content / group / party。
//   - ルートの controller には、doc.go・routes.go・helpers.go（複数の機能が使う共通のもの）と、
//     1 ファイルだけの機能（health, user, block, me, notification, device, schedule, stats, ws）だけを置く。
//     ルートは機能フォルダーを import しない（循環を作らない）。機能フォルダーはルートを import してよい。
//   - ファイル名からは機能の接頭辞を落とす（group_invite.go → group/invite.go）。
//   - フォルダーをまたいで使う名前は公開する（先頭を大文字。PageLimit など）。
//
// import の別名:
//   - 共通部分はルートの controller（"github.com/applemango/chain/internal/controller"）。
//   - service の機能フォルダーを使うときは <名前>svc（例 groupsvc "github.com/applemango/chain/internal/service/group"）。
//   - service の共通部分は core（"github.com/applemango/chain/internal/service/core"）。
package controller
