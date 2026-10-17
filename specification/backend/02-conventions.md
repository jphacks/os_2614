# 02 共通規約

## 1. HTTP API

- ベースパス: `/api/v1`
- 本文: JSON（UTF-8）。キーは camelCase。メディアのアップロードのみ `multipart/form-data`
- 認証: `Authorization: Bearer <access JWT>`（03 §3）。認証不要なのは `/auth/*`、OAuth メタデータ、`/health` だけ
- 冪等性: 作成系の POST は任意の `Idempotency-Key` ヘッダを受け付ける。同じキーでの 24 時間以内の再送には、最初のレスポンスをそのまま返す（Redis `idem:{did}:{key}`）

## 2. 識別子と時刻

| 対象 | 形式 |
| --- | --- |
| ユーザー | DID（`did:plc:...` / `did:web:...`）。API では handle ではなく DID で指す |
| backend が作る行 | UUIDv7 の文字列 |
| instance の record | Spaces 形式の AT URI と rkey（TID）（10 §2） |
| 時刻（JSON） | RFC 3339、UTC、ミリ秒まで（`2026-10-17T03:12:45.123Z`） |
| 時刻（SQLite） | unix ミリ秒の INTEGER |
| 日付の区切り | 集計・ダイジェストは Asia/Tokyo の暦日 |

2 人の組を表す行（すれ違い、フレンド）は、DID を文字列比較して小さい方を `user_a`、大きい方を `user_b` に入れる。

## 3. エラー

```json
{ "error": { "code": "not_in_geofence", "message": "グループのエリア外です", "details": {} } }
```

| HTTP | code | 用途 |
| --- | --- | --- |
| 400 | `invalid_request` | 形式・値域の検証エラー。`details.fields` に項目別の理由を入れる |
| 401 | `unauthenticated` | トークンなし・期限切れ |
| 401 | `atproto_session_expired` | PDS 側のセッションが失効した。再ログインが必要 |
| 403 | `atproto_reauth_required` | 必要な scope がない（scope を足した後の既存ユーザー）。再ログインが必要（17 §2.4） |
| 403 | `forbidden` | 権限なし |
| 403 | `not_in_geofence` | 位置条件を満たさない |
| 403 | `location_stale` | 判定に使える新しい位置がない（05 §4） |
| 403 | `blocked` | ブロック関係にある |
| 403 | `consent_required` | 投稿内容の自動判定への同意がない（§8） |
| 404 | `not_found` | 存在しない、または見えない（存在を漏らさないため 403 ではなく 404 を返す場合がある） |
| 409 | `conflict` | 状態遷移の競合（既にフレンド、既に party に所属など） |
| 410 | `gone` | 期限切れの招待コード・archive 済みの instance への書込 |
| 413 | `payload_too_large` | メディアのサイズ超過 |
| 422 | `moderation_blocked` | moderation で拒否された（11） |
| 429 | `rate_limited` | `Retry-After` ヘッダを付ける |
| 502 | `upstream_error` | PDS・NanoGPT・FCM の失敗 |

## 4. ページング

- 一覧はカーソル方式。`?limit=`（既定 30、最大 100）と `?cursor=`
- レスポンスは `{ "items": [...], "cursor": "..." }`。次ページがなければ `cursor` を省略する
- カーソルは `(sort_key, id)` を base64url にした不透明な文字列

## 5. レート制限

Redis の固定窓カウンタ（`rl:{bucket}:{did}:{window}`）で、DID 単位に制限する。

| bucket | 上限 | 対象 |
| --- | --- | --- |
| `sightings` | 6 回 / 分 | `POST /encounters/sightings` |
| `presence` | 12 回 / 分 | `POST /party/presence` |
| `location` | 6 回 / 分 | `PUT /me/location` |
| `post` | 30 回 / 分 | instance・公開投稿の作成 |
| `media` | 20 回 / 分 | メディアのアップロード（アイコン・バナーの差し替えを含む） |
| `friend_req` | 30 回 / 時 | フレンド申請 |
| `default` | 300 回 / 分 | その他すべて |

`/auth/*` は IP 単位で 30 回 / 分とする。

## 6. 設定値（環境変数）

| 変数 | 必須 | 例 / 既定 | 内容 |
| --- | --- | --- | --- |
| `CHAIN_ENV` | | `dev` | `dev` / `prod` |
| `CHAIN_LISTEN` | | `:8080` | |
| `CHAIN_PUBLIC_URL` | ✓ | `https://chain.i32.jp` | OAuth の client_id とコールバックの基点 |
| `CHAIN_APP_REDIRECT` | ✓ | `chain://auth/callback` | ログイン完了後にアプリへ戻す deep link |
| `CHAIN_DB_PATH` | | `data/app.db` | |
| `CHAIN_REDIS_URL` | ✓ | `redis://localhost:6379/0` | |
| `CHAIN_MEDIA_DIR` | | `data/media` | |
| `CHAIN_SECRET_KEY` | ✓ | 32 バイトの base64 | DB 内の秘密情報を暗号化する AES-256-GCM 鍵 |
| `CHAIN_JWT_KEY` | ✓ | 32 バイト以上の base64 | アクセス JWT（HS256）と署名付きメディア URL の鍵 |
| `CHAIN_OAUTH_PRIVATE_KEY` | prod で ✓ | P-256 秘密鍵の multibase（`go run ./cmd/keygen`） | OAuth confidential client の `private_key_jwt` 用。dev で未設定なら loopback クライアントになる（03 §2.1.1） |
| `CHAIN_OAUTH_SCOPES` | | `atproto repo:jp.i32.chain.feed.post repo:jp.i32.chain.actor.profile repo:app.bsky.feed.post?action=create&action=delete repo:app.bsky.actor.profile?action=create&action=update blob:image/*` | 要求する scope（空白区切り）。granular scope が通らない場合は `atproto transition:generic` にする |
| `CHAIN_JETSTREAM_URL` | | `wss://jetstream2.us-east.bsky.network/subscribe` | |
| `CHAIN_NANOGPT_URL` | | `https://api.nano-gpt.com/api/v1/decisions` | NanoGPT Decisions API（11 §5） |
| `CHAIN_NANOGPT_KEY` | | | 未設定なら moderation は常に `ok`、Pin は常に `false` を返す（開発用） |
| `CHAIN_JEV_MODEL` | | `typesafe/jev-1.13` | 判定モデル。再現性のため `jev-latest` ではなく版を固定する |
| `CHAIN_FCM_CREDENTIALS` | | サービスアカウント JSON のパス | 未設定なら通知はログ出力のみ |
| `CHAIN_EMBED_URL` | | `http://127.0.0.1:11434/v1/embeddings` | 自動グループの embedding（09 §8.3）。OpenAI 互換の推論サーバ。未設定なら自動グループの照合をしない |
| `CHAIN_EMBED_MODEL` | | `embeddinggemma` | embedding のモデル |
| `CHAIN_EMBED_DIM` | | `256` | 使う次元数 |
| `CHAIN_CORS_ORIGINS` | | `https://localhost,capacitor://localhost,http://localhost:3000` | 許可するオリジン（カンマ区切り） |

近接判定・party などの調整値は、各仕様書の「パラメータ」表にある既定値を `config` パッケージの定数として持つ。環境変数では上書きしない（実機調整のときにだけコードで変える）。

## 7. ログと監査

- `slog` の JSON 出力。リクエストごとに `request_id`、`did`、`route`、`status`、`latency_ms` を出す
- 位置座標、beacon token、投稿本文はログに出さない
- moderation の拒否、ブロック、アカウント削除は `audit_log` 表に記録する（04）

## 8. 通信の秘密

グループ / Party 内の投稿は電気通信事業法の届出対象として扱う（README）。
以下は設計上の前提であり、法的な判断は届出の準備で確認する（本仕様は法的助言ではない）。

| 対象 | 扱い |
| --- | --- |
| Space 内の投稿・写真の本文 | **運営者が閲覧する機能を作らない**（管理画面・管理 API に本文を出さない）。調査が必要な場合は、通報（11 §6）で通報者が示した投稿に限る |
| 誰が誰と同じ Space にいるか（通信の構成要素） | 本文と同じく秘密として扱う。統計（13 §3）には個人を特定できない集計だけを出す |
| 自動判定（moderation / Pin） | 投稿の本文を外部 API（NanoGPT）に送るため、利用規約とは別に**個別の明示的な同意**を初回起動時に取る（README Q9）。同意の版と時刻を `user_settings` に記録する |
| 同意しないユーザー | Space 内の投稿はできない（閲覧と公開投稿はできる）。moderation を外して投稿させると、block の保証が崩れるため |
| ログ | 本文・宛先の Space をログに出さない（§7） |
| 外部 API への送信内容 | 本文と最小限の文脈（投稿の種類）だけを送る。DID・handle・Space 名は送らない（11 §5.2） |

同意を記録するため、`user_settings` に `content_scan_consent_version INTEGER` と `content_scan_consent_at INTEGER` を追加する（04）。
