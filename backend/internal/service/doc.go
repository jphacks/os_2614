// Package service は業務ロジックを持つ。
//
// 規則:
//   - 1 つの機能につき 1 ファイル・1 つの型（EncounterService など）。
//   - 依存は Deps と、外部サービス（internal/external/*）の interface で受け取る。
//     時刻は Deps.Clock、ID と乱数は Deps.IDs から取る（time.Now を直接呼ばない）。
//   - トランザクションの境界はここで決める（db.Transaction）。
//   - API に返すエラーは httpx.Err*（必要なら .With で details を足す）。それ以外のエラーは 500 になる。
//   - Fiber の型（fiber.Ctx）に依存しない。controller から呼ばれる純粋な Go の関数にする。
//
// フォルダーの分け方:
//   - 複数のファイルを持つ機能は、機能ごとのフォルダー（＝ Go のパッケージ）に分ける。
//     機能フォルダーは proximity / moderation / schedule / notify / access / content /
//     encounter / auth / friend / me / group / party / natural。
//   - いちばん下の core に、共通部分（Deps・params.go・geohash.go・image.go・common.go）を置く。
//     core はどの機能フォルダーからも import できる。
//   - 機能フォルダーはレベルの順にしか import しない（循環を作らない）:
//     core ← proximity, moderation, schedule, notify ← access ← content ← encounter, auth, friend
//     ← me, group, party ← natural。
//   - ルートの service には、doc.go と 1 ファイルだけの機能（health, user, block, janitor）
//     だけを置く。ルートは機能フォルダーを import しない。
//   - ファイル名からは機能の接頭辞を落とす（group_invite.go → group/invite.go）。
//   - フォルダーをまたいで使う名前は公開する（先頭を大文字）。
//
// import の別名:
//   - 共通部分は core（"github.com/applemango/chain/internal/service/core"）。
//   - 同じ層のルートはそのまま service、機能フォルダーは本来の名前（group など）。
//   - 別の層（controller など）から機能フォルダーを使うときは <名前>svc
//     （例 groupsvc "github.com/applemango/chain/internal/service/group"）。
package service
