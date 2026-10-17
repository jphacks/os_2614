// Package controller は Fiber のハンドラを持つ。
//
// 規則:
//   - 1 つの機能につき 1 ファイル・1 つの型（EncounterController など）。
//     型は自分が使う service だけをフィールドに持ち、Register でルートを登録する。
//   - やることは「入力を読む（httpx.Bind）→ service を呼ぶ → レスポンスを返す」だけ。業務ロジックを書かない。
//   - リクエスト / レスポンスの JSON の型はこのパッケージに置き、model の型をそのまま返さない
//     （DB の列名や内部の値を API に漏らさないため）。
//   - エラーは service から返ったものをそのまま return する。形式は httpx.ErrorHandler が整える。
package controller
