# 01 全体構成

## 1. 構成要素

```
[Android app (Capacitor)]
  ├─ WebView: Next.js static export ── HTTPS (REST) / WSS ──┐
  └─ Native : BLE advertise/scan, 位置, FGS ── (JS bridge) ─┘
                                                            ▼
                                   ┌──────────── chain-backend (Go, 単一バイナリ) ────────────┐
                                   │ Fiber v3 HTTP / WebSocket                                  │
                                   │  ├ auth (ATProto OAuth BFF, app JWT)                       │
                                   │  ├ proximity (BLE / GPS 共通)                               │
                                   │  ├ encounter / friend / location / party / group           │
                                   │  ├ instance (Spaces 同形の record store) / media / pin     │
                                   │  ├ moderation / notify / stats                              │
                                   │  └ workers: jetstream, party engine, notifier, cleanup     │
                                   └──────┬──────────────┬───────────────┬──────────────────────┘
                                          │              │               │
                                    SQLite (WAL)       Redis       data/media (ファイル)
                                          │
              外部: ユーザーの PDS（OAuth + DPoP で書込）/ Jetstream（読込）/ NanoGPT（Jev） / FCM
```

- backend は 1 プロセスで動かす。SQLite の書き込みは単一プロセス前提のため、水平スケールはしない。
- 外部から到達できる HTTPS の公開 URL が必須である（ATProto OAuth の client_id とリダイレクト先になるため）。開発時はトンネル（cloudflared 等）を使う。

## 2. プロセス内の常駐ワーカー

すべて `main.go` から goroutine として起動し、`context` のキャンセルで停止する。

| ワーカー | 周期 / 契機 | 役割 | 仕様 |
| --- | --- | --- | --- |
| `jetstream.Consumer` | 常時接続 | `jp.i32.chain.*` と `app.bsky.actor.profile` を購読し索引する | 03 §5 |
| `party.Engine` | 5 秒 tick | フレンド近接グラフから party の状態遷移を計算する | 08 |
| `notify.Sender` | outbox ポーリング 2 秒 | FCM 送信と再試行 | 12 |
| `notify.Digest` | 毎日 21:00 JST | すれ違いダイジェストを積む | 12 |
| `encounter.Revealer` | 1 分 tick | `reveal_at` に達したすれ違いを公開し通知する | 06 |
| `cleanup.Janitor` | 10 分 tick | 期限切れ token・招待・セッション・メディアの掃除 | 04 §4 |
| `group.Clusterer` | 5 分 tick（P2） | 自然発生グループの候補抽出 | 09 §8 |

## 3. ディレクトリ構成

層ごとにパッケージを分け（controller / service / model）、各層の中を機能ごとのフォルダーに分ける。
Go ではフォルダー＝パッケージなので、**フォルダー同士が循環しないように、import できる向きを決めてある**。
機能ごとのファイルとパスの一覧は 18（`make codemap` で生成）。

```
backend/
  main.go                  設定読込、依存の組み立て、ルーティング、起動と停止
  cmd/dbreset/  cmd/keygen/
  lexicons/  scripts/  Makefile  compose.yaml  .env.example
  internal/
    controller/            Fiber のハンドラ
      routes.go helpers.go   ルートの登録と、複数の機能が使う共通のもの（PageLimit など）
      health.go user.go block.go me.go notification.go device.go schedule.go stats.go ws.go   1 ファイルの機能
      auth/  encounter/  friend/  content/  group/  party/
    service/               業務ロジック
      health.go user.go block.go janitor.go   1 ファイルの機能
      core/                共通部分（Deps、params、geohash、画像の処理、複数の機能が使う型と定数）
      proximity/  moderation/  schedule/  notify/      レベル 1
      access/                                          レベル 2
      content/                                         レベル 3（instance・Pin・メディア・フィード・公開投稿・Jetstream・WebSocket・再判定）
      encounter/  auth/  friend/                       レベル 4
      me/  group/  party/                              レベル 5
      natural/                                         レベル 6（自動グループ）
    model/                 データの型と SQLite / Redis への読み書き
      helpers.go             共通の小さな関数と定数
      audit.go beacon.go ... user_handle.go   1 ファイルの機能（20 個）
      encounter/  friend/  space/  group/  schedule/   機能フォルダー
      maintenance/         アカウント削除と掃除（多くの表をまたぐ）
    worker/                常駐ワーカー（すべて 1 ファイルなので分けない）
    realtime/  storage/  external/{atproto,embed,fcm,nanogpt}/
    httpx/  auth/  db/  kv/  config/  clock/  idgen/  utils/  testutil/
```

### 3.1 フォルダーの分け方の規則

| 規則 | 内容 |
| --- | --- |
| 機能フォルダー | 複数のファイルを持つ機能は、機能ごとのフォルダーに分ける。ファイル名からは機能の接頭辞を落とす（`group_invite.go` → `group/invite.go`） |
| 層の直下 | 共通部分と、ほかの機能のフォルダーを使わない 1 ファイルの機能だけを置く。直下のパッケージは機能フォルダーを import しない |
| import の向き | service はレベルの小さい方にしか import しない（`core` ← レベル 1 ← … ← レベル 6）。model は直下 ← 機能フォルダー ← `maintenance`。controller の機能フォルダーは直下を import してよい |
| 公開する名前 | フォルダーをまたいで使う名前は公開する（先頭を大文字） |
| テスト | コードと同じフォルダーに置く。外部テストのパッケージ名は `<機能>_test`。複数の機能のテストが共有するヘルパーは `internal/testutil/` |

### 3.2 import の別名

層をまたぐと同じ名前のパッケージがある（`service/group` と `model/group` など）ので、別名を決めてある。

| 使う側 → 使われる側 | 別名 | 例 |
| --- | --- | --- |
| 同じ層の機能フォルダー | 付けない | `group` |
| service → model の機能フォルダー | `<名前>model` | `groupmodel "…/internal/model/group"` |
| controller・testutil → service の機能フォルダー | `<名前>svc` | `groupsvc "…/internal/service/group"` |
| main.go・testutil → controller の機能フォルダー | `<名前>ctrl` | `groupctrl "…/internal/controller/group"` |
| service の共通部分 | `core` | `"…/internal/service/core"` |

依存の向き（層の間）は次のとおりで、逆向きの import をしない。

```
controller ──▶ service ──▶ model ──▶ db / kv
     │            │
     └─▶ httpx    ├─▶ external/*（service 側で定義した interface 越し）
                  ├─▶ realtime（Broker interface 越し）
                  └─▶ storage
worker ──▶ service
```

| 層 | 書くこと | 書かないこと |
| --- | --- | --- |
| controller | 入力の読み取りと検証（`httpx.Bind`）、service の呼び出し、レスポンスの整形 | 業務ロジック、SQL |
| service | 権限・状態遷移・しきい値などの業務ルール、トランザクション境界、`httpx.Err*` の選択 | Fiber の型、SQL の文字列 |
| model | 行に対応する型、SQL と Redis のコマンド | 業務ルール |

各層の細かい規則は、それぞれの `doc.go` に書いてある。

## 4. 採用ライブラリ

| 用途 | ライブラリ | 備考 |
| --- | --- | --- |
| HTTP | `github.com/gofiber/fiber/v3` | 既存 |
| WebSocket | `github.com/gofiber/contrib/v3/websocket` | v3 対応版のモジュールパスは導入時に要確認 |
| SQLite | `github.com/mattn/go-sqlite3` | 既存。cgo 必須 |
| Redis | `github.com/redis/go-redis/v9` | |
| ATProto | `github.com/bluesky-social/indigo`（`atproto/auth/oauth`、`atproto/syntax`、`atproto/identity`） | OAuth クライアントの API 形状は導入時に要確認 |
| JWT | `github.com/golang-jwt/jwt/v5` | アプリのアクセストークン |
| ID | `github.com/google/uuid`（UUIDv7） | 既存の indirect 依存 |
| FCM | `firebase.google.com/go/v4/messaging` | |
| 画像 | 標準 `image/jpeg`、`image/png`、`golang.org/x/image/webp`、`golang.org/x/image/draw` | 再エンコードで EXIF を落とす |
| ログ | 標準 `log/slog` | JSON 出力 |

## 5. 既存コードからの変更点

`utils/array.go` 以外は、土台の実装（16 の S0）で対応済み。

| 対象 | 変更 | 状態 |
| --- | --- | --- |
| `go.mod` | module 名を `github.com/applemango/todo` から `github.com/applemango/chain` に変えた | 済 |
| `db/sqlite3.go` | 接続をパッケージ変数に固定せず、`Open(path)` で作って関数の引数（`db.Querier`）で渡す形にした | 済 |
| 同上 | DSN で PRAGMA（`journal_mode=WAL`、`foreign_keys`、`busy_timeout=5000`、`synchronous=NORMAL`）をプールの全接続に設定する | 済 |
| 同上 | `GetItems` に `defer rows.Close()` と `rows.Err()` の確認を足し、Scan のエラーを握りつぶさずに返すようにした。結果が 0 件なら `nil` ではなく空のスライスを返す | 済 |
| 同上 | `GetItem` は `sql.ErrNoRows` をそのまま返す | 済 |
| 同上 | `Transaction` / `TransactionExecut` を、関数を受け取ってコミットとロールバックを行う `Transaction(ctx, conn, fn)` にまとめた | 済 |
| `utils/array.go` | `Map` は要素の 1 つでエラーが出ると全体で `nil` を返す。この挙動を doc comment に明記するか、`([]Y, error)` を返す形にする | 未 |
| `db/schema.sql`、`db/seed.sql` | 新設。`schema.sql` は 04 §2 の DDL をそのまま抜き出したもの。04 §5 の方針で作り直す | 済 |
| `main.go` | `/api` の hello を消し、`/health` と `/api/v1` 配下の構成にした。CORS は `CHAIN_CORS_ORIGINS`（既定は Capacitor のオリジンと `http://localhost:3000`）だけを許可する | 済 |

## 6. 開発環境の公開 URL（Cloudflare Tunnel）

ATProto OAuth の client_id とコールバックには、外部から HTTPS で届く URL が要る（03 §2）。開発中は Cloudflare Tunnel で手元の backend を公開する。

| 項目 | 値 |
| --- | --- |
| 公開ホスト | `https://chain.i32.jp`（DNS は Tunnel への proxied CNAME） |
| Tunnel | 名前 `chain`。設定は Cloudflare 側で管理する（remote-managed） |
| ingress | `chain.i32.jp` → `http://localhost:8080`、それ以外は 404 |
| 起動 | `backend/scripts/tunnel.sh`。token はリポジトリに置かず、実行のたびに `cf tunnels token get` で取得する |
| 前提 | `cf` CLI でログイン済みであること。外向きの 7844 番ポート（TCP / UDP）が通るネットワークであること |

- `CHAIN_PUBLIC_URL=https://chain.i32.jp` とする
- Tunnel 経由の接続元は、backend からは手元の cloudflared（ループバック）に見える。ループバックから来たリクエストに限って `CF-Connecting-IP` を信頼し、レート制限（02 §5）などで本来の接続元の IP を使う（`main.go` の `ProxyHeader` / `TrustProxy`）
- 同じ Tunnel を複数の人が同時に張ると、リクエストが各人の手元に振り分けられてしまう。複数人で開発するときは、人ごとに Tunnel とホスト名（例: `chain-{name}.i32.jp`）を足す
