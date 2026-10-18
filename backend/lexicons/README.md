# lexicons

`jp.i32.chain.*` の lexicon JSON を置く（specification/backend/03-auth-atproto.md §6、19-openness.md §6.1）。
ファイル名は NSID をそのまま使う（例: `jp.i32.chain.feed.post.json`）。
`lexicons.go` が `go:embed` で読み込み、書き込み前と Jetstream の取り込みの検証に使う
（`internal/external/atproto/lexicon.go`）。

- `deps/` は参照先の `com.atproto.*` の写し（indigo の `lexicons/` から）。検証のために catalog に入れるだけで、公開はしない
- 外部には公開しない（O6 まで）。公開するまでは形を変えてよい
