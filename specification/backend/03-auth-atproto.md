# 03 認証と ATProto 連携

## 1. 役割

backend は ATProto に対して次の 3 つの役割を持つ。

| 役割 | 内容 |
| --- | --- |
| OAuth confidential client（BFF） | ユーザーの PDS のトークンと DPoP 鍵は backend だけが持つ。アプリには渡さない |
| AppView | Jetstream から `jp.i32.chain.*` を索引し、フィード・プロフィールを返す |
| space authority（将来） | グループ / Party の Space を管理する DID。P0 では DB 上の概念として持つだけ（§7） |

アプリ ↔ backend の認証には、ATProto のトークンとは別のアプリセッション（§3）を使う。

## 2. ログインフロー（P0）

```
App                       Backend                         PDS / 認可サーバ
 │ 1. Browser.open(/auth/login?handle=alice.bsky.social)
 │──────────────────────────▶│ 2. handle → DID → PDS → 認可サーバ解決
 │                           │ 3. PAR (PKCE, DPoP, private_key_jwt) ──────▶│
 │◀───── 302 認可画面 ───────────────────────────────────────────────────│
 │ 4. ユーザーが承認                                                        │
 │──────────── 302 /auth/callback?code&state&iss ─▶│                       │
 │                           │ 5. token 交換（DPoP）────────────────────────▶│
 │                           │ 6. users / atproto_oauth_sessions を保存
 │                           │    one-time code を Redis に保存（60 秒）
 │◀── 302 chain://auth/callback?code=<otc> ──│
 │ 7. POST /auth/exchange {code}
 │──────────────────────────▶│ 8. app_sessions を作成
 │◀── {accessToken, refreshToken, did} ──│
```

- 1 の `handle` は省略可。省略時は認可サーバとして `https://bsky.social` に PAR を投げ、そこで handle を入力させる
- 2 以降のプロトコル処理（解決、PAR、PKCE、DPoP nonce の追従、トークン交換、`iss` と DID の照合）は indigo の `atproto/auth/oauth`（`ClientApp`）に任せる。保存先は `ClientAuthStore` を自前で実装する: 認可要求は Redis の `oauth:req:{state}`（TTL 10 分、暗号化）、セッションは SQLite の `atproto_oauth_sessions`（暗号化）
- 5 で得た `sub`（DID）と `iss` の照合は indigo が行う（S1 で確認済み）
- アプリへの戻りは開発時はカスタムスキーム `chain://`、本番は App Links（`https://{host}/app/auth/callback`）にする。one-time code を URL に載せるのは、アプリ用トークンを URL に載せないため

### 2.1 client metadata

`GET /oauth-client-metadata.json`（= client_id）で返す。

```json
{
  "client_id": "{PUBLIC_URL}/oauth-client-metadata.json",
  "client_name": "CHAIN",
  "application_type": "web",
  "grant_types": ["authorization_code", "refresh_token"],
  "response_types": ["code"],
  "redirect_uris": ["{PUBLIC_URL}/auth/callback"],
  "scope": "atproto repo:jp.i32.chain.feed.post repo:jp.i32.chain.actor.profile",
  "token_endpoint_auth_method": "private_key_jwt",
  "token_endpoint_auth_signing_alg": "ES256",
  "jwks_uri": "{PUBLIC_URL}/oauth/jwks.json",
  "dpop_bound_access_tokens": true
}
```

- scope は書き込むコレクションだけに絞った granular scope にする。認可サーバが granular scope を受け付けない場合に限り、`transition:generic` に落とす（設定値で切替）
- Spaces 統合時には `space:` scope を追加する（§7）。scope を足すと、既存ユーザーには再認可が必要になる

### 2.1.1 クライアントの方式

| 方式 | 条件 | client_id | コールバック |
| --- | --- | --- | --- |
| confidential | `CHAIN_OAUTH_PRIVATE_KEY`（P-256 の multibase）がある | `{PUBLIC_URL}/oauth-client-metadata.json` | `{PUBLIC_URL}/auth/callback` |
| loopback（開発用） | 鍵がなく、`CHAIN_ENV=dev` | `http://localhost?redirect_uri=...&scope=...`（ATProto の localhost 開発用クライアント） | `http://127.0.0.1:{port}/auth/callback` |

- loopback は public client なので、refresh の寿命が短い。Tunnel なしで PC のブラウザからログインを試すためのもの。端末からは使えない（127.0.0.1 が端末自身を指すため）
- 鍵は `go run ./cmd/keygen` で作る。prod では鍵が必須

### 2.1.2 開発用の完了画面

`CHAIN_ENV=dev` のときだけ、`/auth/login?return=dev` で始めたログインは、アプリの deep link ではなく `/auth/dev/complete?code=...` に戻る。この画面は one-time code を交換して、アプリ用トークンを JSON で表示する（PC のブラウザで試すため）。どちらに戻すかは、ログイン開始時に付ける cookie `chain_return` で区別する。

### 2.2 ATProto セッションの保存

- `atproto_oauth_sessions`（04）に、PDS のアクセス / リフレッシュトークン、DPoP 秘密鍵、DPoP nonce を保存する
- indigo の `ClientSessionData`（トークン、DPoP 鍵、nonce を含む）を JSON にして、`CHAIN_SECRET_KEY` による AES-256-GCM で暗号化し、1 列に保存する
- PDS のアクセストークンの期限が切れていれば、PDS 呼び出しの直前にリフレッシュする。同じユーザーへの同時リフレッシュは、Redis のロック `lock:atp-refresh:{did}`（TTL 15 秒）で 1 本に絞る
- リフレッシュに失敗したら（期限切れ・取り消し）そのセッションを失効させる。PDS への書込が要る API は `401 atproto_session_expired` を返す。PDS を使わない API（すれ違い、party など）はアプリセッションが生きていれば使い続けられる

## 3. アプリセッション（P0）

| トークン | 形式 | 有効期間 | 保存 |
| --- | --- | --- | --- |
| access | JWT HS256。claims は `sub`=DID、`sid`=セッション ID、`iat`、`exp` | 15 分 | 保存しない |
| refresh | 32 バイト乱数の hex（既存 `auth.GenerateRandomHex(32)`） | 最終使用から 30 日 | `app_sessions.refresh_hash` に SHA-256（既存 `auth.GenerateSha256`） |

- `POST /auth/refresh {refreshToken}` で新しい access と refresh を返し、古い refresh は無効にする（ローテーション）
- 無効化済みの refresh が再提示されたら漏洩とみなし、同じ `family_id` のセッションをすべて失効させる
- 認証ミドルウェアは JWT を検証した後、Redis `sess:{sid}`（TTL 15 分）で失効していないかを確認する。キャッシュがなければ SQLite を見る
- `POST /auth/logout` は自分のセッションを失効させる。ATProto セッションは他の端末と共有しているため、最後のアプリセッションが消えたときにだけ取り消す
- アプリ側のトークンは **native 層だけが持つ**（Android Keystore の鍵で暗号化して保存）。WebView は native から access token を受け取るだけにする。refresh を 1 か所に直列化しないと、同時のローテーションが再利用検知に引っかかってセッションが失効するため（`specification/android` §3）

## 4. PDS への書き込み（P1）

- `atproto.Writer` が `com.atproto.repo.createRecord` / `deleteRecord` / `putRecord` を、ユーザーの OAuth セッション（DPoP）で呼ぶ
- 書いた直後に、レスポンスの `uri` と `cid` でローカルの索引に先に入れる（read-after-write のため）。後から Jetstream で同じ `uri`・`cid` が届いたら何もしない
- 投稿前の moderation（11）は PDS に書く前に行う。拒否された内容は PDS に書かない

## 5. Jetstream 購読（P1）

- 接続先: `{CHAIN_JETSTREAM_URL}?wantedCollections=jp.i32.chain.*&wantedCollections=app.bsky.actor.profile&cursor={cursor}`
- `users` 表にない DID のイベントは捨てる（CHAIN 未登録ユーザーの記録は索引しない）
- 処理するイベント

| kind / collection | 処理 |
| --- | --- |
| commit create/update `jp.i32.chain.feed.post` | lexicon で検証して `public_posts` に upsert。非同期で moderation に回す |
| commit delete `jp.i32.chain.feed.post` | `public_posts.deleted_at` を立てる |
| commit `jp.i32.chain.actor.profile` / `app.bsky.actor.profile` | `profiles` を更新する |
| identity | `users.handle` を更新する |
| account（`active=false`） | そのユーザーの公開投稿を非表示にする |

- 処理済みイベントの `time_us` を 5 秒ごとに `jetstream_cursor` へ保存する。再接続時は保存値の 5 秒前から再開し、重複は upsert で吸収する
- 切断時は指数バックオフ（1 秒から 2 倍、上限 60 秒）で再接続する

## 6. lexicon

NSID の authority は所有ドメイン `chain.i32.jp` を逆順にした `jp.i32.chain` とする。`backend/lexicons/` に JSON で置き、`indigo` の lexicon 検証に使う。

- lexicon を外部に公開する場合（P2）は、NSID の authority 部分（例: `jp.i32.chain.feed`）を逆順にしたドメインに DNS TXT `_lexicon.feed.chain.i32.jp` を置き、lexicon の schema record を載せた repo の DID を指す
- 公開した lexicon は後方互換のない変更ができない。P0〜P1 の間は公開せず、Spaces 統合までに確定させる

| NSID | 種別 | 保存先 | 優先度 |
| --- | --- | --- | --- |
| `jp.i32.chain.feed.post` | record（key: tid） | ユーザーの PDS（公開） | P1 |
| `jp.i32.chain.actor.profile` | record（key: literal:self） | ユーザーの PDS（公開） | P1 |
| `jp.i32.chain.space.group` | space type | backend SQLite（将来 Space） | P0 |
| `jp.i32.chain.space.party` | space type | backend SQLite（将来 Space） | P0 |
| `jp.i32.chain.instance.post` | record（key: tid） | Space 内（P0 は SQLite） | P0 |
| `jp.i32.chain.instance.pin` | record（key: tid）。authority が書く | Space 内（P0 は SQLite） | P1 |
| `jp.i32.chain.instance.media` | record（key: tid）。Party の自動共有写真 | Space 内（P0 は SQLite） | P0 |

`jp.i32.chain.feed.post`

```json
{
  "text": "string, maxGraphemes 1000, maxLength 10000",
  "langs": ["ja"],
  "createdAt": "datetime"
}
```

`jp.i32.chain.actor.profile`（Bluesky のプロフィールにない CHAIN 固有の公開情報だけを持つ）

```json
{ "status": "string, maxGraphemes 40", "createdAt": "datetime" }
```

`jp.i32.chain.instance.post` の定義は 10 §3 に書く。

## 7. Spaces との整合

### 7.1 方針

ATProto Spaces（proposal `bluesky-social/proposals/0016-permissioned-data`、2026-08-20 alpha 公開）を、グループ / Party の保存先として最終的に採用する。
alpha の段階では本番に使えないため、P0 ではデータモデル・URI・権限・アクセス判定を Spaces と同じ形にして SQLite で実装する。

| Spaces の概念 | CHAIN の P0 実装 |
| --- | --- |
| space authority（DID） | backend のサービス DID `did:web:{API_HOST}`（README Q8）。全 Space で共通 |
| space type（NSID） | `jp.i32.chain.space.group` / `jp.i32.chain.space.party` |
| space key（skey） | `spaces.id`（UUIDv7） |
| space-ref | `at://{authority}/space/{type}/{skey}` を `spaces.ref` として保存 |
| space 内の record URI | `at://{authority}/space/{type}/{skey}/{authorDid}/{collection}/{rkey}`。`records.uri` |
| 投稿者ごとの repo | `records.author_did` で分割。外部には repo 単位の API を出さない |
| `com.atproto.simplespace` のメンバー（read / write の真偽値） | `space_members.can_read` / `can_write` |
| read / write policy（`public` / `member-list` / `managing-app`） | `spaces.read_policy` / `write_policy`。CHAIN は両方 `managing-app` |
| `checkUserAccess` | `instance.AccessChecker.Check(ctx, spaceID, did, mode)`（10 §5）。geofence・在席・招待の判定はすべてここに集める |
| 短命の read credential（既定 10 分） | アクセス判定の結果を Redis `acc:{space}:{did}:{mode}` に最大 10 分キャッシュする。失効もこの時間で効く |
| Space の削除 | `spaces.state='deleted'`。record は残すが誰も読めなくする |
| 公開ラベルを使わない moderation | moderation の結果は `records.mod_label` に持ち、Space の外（公開ラベラー）には出さない |

この表の右列を守っていれば、Spaces 統合時に `InstanceStore` の実装を差し替えるだけで済む。

### 7.2 統合時の作業（P2）

PDS 自体の運用は軽い。Docker イメージ `ghcr.io/bluesky-social/atproto:pds-spaces-alpha` は追加設定なしで Spaces を有効にできると告知されている。
手間がかかるのは次の 2 点である。

1. **backend の Spaces クライアント実装（Go）**。alpha の SDK は TypeScript の `@atproto` だけで、indigo には対応がない。次を自前で実装するか、TypeScript の補助プロセスを置く
   - authority 側: DID ドキュメントに `#atproto_space` 鍵と `#atproto_space_host` を出す。`getSpaceCredential` で、`checkUserAccess` の結果に基づく資格を発行する
   - 読み取り側（AppView として同期）: `getDelegationToken` → P-256 `did:key` で署名した HTTP Message Signature → `getSpaceCredential` → `Authorization: Atproto-Space` と `Atproto-Space-Audience` で各 repo を読む
   - 同期: `listRepos` で書き手の集合、`listRepoOps` で増分、`getRepo`（CAR）で全量回復、LtHash による整合確認
2. **ユーザーの書き込み先**。Space の record は投稿者自身の PDS に書かれる。bsky.social が Spaces に対応するまでは、bsky.social のユーザーは Space に書けない。統合後もしばらくは次の 2 経路を併存させる
   - Spaces 対応 PDS のユーザー: 自分の PDS に書く（`space:` scope が必要）
   - 未対応 PDS のユーザー: 従来どおり backend の SQLite に書く

`InstanceStore` は次の形で定義し、P0 は `SQLiteStore` だけを実装する。

```go
type InstanceStore interface {
	CreateRecord(ctx context.Context, space SpaceRef, author syntax.DID, collection syntax.NSID, value any) (Record, error)
	DeleteRecord(ctx context.Context, uri syntax.ATURI) error
	GetRecord(ctx context.Context, uri syntax.ATURI) (Record, error)
	ListRecords(ctx context.Context, space SpaceRef, collection syntax.NSID, cursor string, limit int) ([]Record, string, error)
}
```

### 7.3 Spaces を使っても変わらない前提

- Spaces が提供するのは**アクセス制御であって機密性ではない**（暗号化されない。サービスは扱う内容を読める）。SQLite 保存と同じく、サーバ運用者は内容を読める前提でプライバシーポリシーを書く
- 読み取り権限は Space 単位の全か無かである。記録単位の閲覧制限（特定メンバーにだけ見せる）は作らない
