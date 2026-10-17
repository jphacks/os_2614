# 16 実装の順番

P0（MVP）を S0〜S9 の順に作り、その後に P1 を足す。
各ステップの「完了条件」を満たしてから次に進む。テストの筋書き（E1 など）は 15 §4。

## 進み具合（2026-10-17 時点）

| ステップ | 状態 | 備考 |
| --- | --- | --- |
| S0 土台 | 済 | |
| S1 検証 | 一部 | indigo の OAuth・granular scope・Fiber v3 の WebSocket（`gofiber/contrib/v3/websocket` v1.1.3）は確認済み。Tunnel の外部からの疎通は未確認 |
| S2 認証・ユーザー | 済 | レート制限は S9 で実装。bsky.social の実アカウントでの最後までのログインは未確認 |
| S3〜S8 | 済 | 各ステップの完了条件のうち、実機・実 API を要するもの（S8 の実機への通知、NanoGPT の実 API）は未確認 |
| S9 仕上げ | 済 | 14 の P0 エンドポイントはすべて実装済み。CI は `.github/workflows/backend.yml`（GitHub 上での実行は未確認） |

結合テストは 380 件すべて通過（失敗・skip なし）。実機 2 台での一連の動作（S9 の完了条件）は未確認。

## 0. 全体像

```
S0 土台 ✔
 └▶ S1 検証（ライブラリ・Tunnel）
     └▶ S2 認証・ユーザー
         ├▶ S3 近接基盤・すれ違い ──▶ S4 フレンド ──▶ S6 Party
         └▶ S5 instance 基盤（投稿・メディア・WebSocket）──┬▶ S6 Party
                                                          └▶ S7 グループ
                                                              └▶ S8 moderation・通知送信
                                                                  └▶ S9 仕上げ ＝ MVP
```

- S3 と S5 は、どちらも S2 だけに依存するので並行して進められる
- Android の native プラグイン（`specification/android`）は、S3 の API ができた時点で並行して着手できる
- **S3 に着手する前に、開発用 API と seed ユーザーのログイン方法（README L1）を決める**。端末なしで sighting を流せないと、frontend との結合が進まないため

各ステップでは、controller / service / model に同じ名前のファイルを作る（例: `controller/encounter.go`、`service/encounter.go`、`model/encounter.go`）。

## S0 土台（済）

| 内容 | 場所 |
| --- | --- |
| 設定の読込と検証 | `internal/config` |
| Clock / ID の注入、テスト用の Fake と Seq | `internal/clock`、`internal/idgen` |
| SQLite の接続と汎用ヘルパー、`schema.sql`、`seed.sql`、`cmd/dbreset` | `internal/db`、`cmd/dbreset` |
| Redis の接続とキー名 | `internal/kv` |
| エラー形式、ErrorHandler、認証ミドルウェア、Bind、Page | `internal/httpx` |
| アクセス JWT、AES-GCM | `internal/auth` |
| 3 層の規則（`doc.go`）、`/health`、ルートの割り当て | `internal/controller`、`internal/service`、`internal/model` |
| Makefile（`run` `env` `db-reset` `test` `lint` `tunnel`）、`compose.yaml`（Redis） | `backend/` |

完了条件: `make env && make redis-up && make db-reset && make run` で `/health` が `{"sqlite":true,"redis":true}` を返す。`make test` と `make lint` が通る。

## S1 検証

仕様のうち、ライブラリの実物を見ないと確定できない点を先に潰す。結果に応じて仕様書を直す。

| 確かめること | 直す仕様 |
| --- | --- |
| indigo の `atproto/auth/oauth` が confidential client（`private_key_jwt`）・DPoP・PAR に対応しているか。セッションの保存先を差し替えられるか | 03 §2、04 `atproto_oauth_sessions` |
| granular scope（`repo:jp.i32.chain.feed.post` など）が bsky.social の認可サーバで通るか | 03 §2.1 |
| Fiber v3 で使える WebSocket のモジュールと、その API | 01 §4、10 §7 |
| 手元のネットワークで Tunnel が繋がり、`https://chain.i32.jp/health` が外から見えるか | 01 §6 |

完了条件: 上の 4 点の結果が仕様書に反映されている。使えないものがあれば代替を決めている。

## S2 認証・ユーザー

| 内容 | 仕様 | 優先度 |
| --- | --- | --- |
| client metadata、JWKS、`/auth/login`、`/auth/callback`、`/auth/exchange` | 03 §2 | P0 |
| アプリセッション: 発行、`/auth/refresh`（ローテーションと再利用検知）、`/auth/logout` | 03 §3 | P0 |
| ATProto セッションの暗号化保存とリフレッシュ（`lock:atp-refresh`） | 03 §2.2 | P0 |
| ログイン時のプロフィール取得 | 13 §1.1 | P0 |
| `GET /me`、`PATCH /me/settings`、`PUT /me/consents`、`GET /users/{did}`（自分のみでよい） | 13 §1、02 §8 | P0 |
| レート制限ミドルウェア | 02 §5 | P0 |

完了条件:
- 実機（または PC のブラウザ）から bsky.social のアカウントでログインでき、`GET /me` が自分のプロフィールを返す
- refresh の再利用で、同じ系列のセッションがすべて失効するテストが通る

## S3 近接基盤・すれ違い

| 内容 | 仕様 | 優先度 |
| --- | --- | --- |
| `PeerProximity`（BLE）と `ZonePresence`（GPS）の interface と実装 | 05 §1、§3、§4.1 | P0 |
| beacon token の発行（`GET /beacon/tokens`） | 05 §2 | P0 |
| 位置の取り込み（`PUT /me/location`） | 05 §4 | P0 |
| sighting の受信、相互判定、統合、公開時刻 | 06 §2〜§5 | P0 |
| `encounter.Revealer`（ワーカー） | 06 §5.4 | P0 |
| `GET /encounters` | 06 §6 | P0 |
| ブロック（`/blocks`）。すれ違いの除外に要るので、ここで作る | 13 §4 | P0 |

完了条件:
- 15 §4.1 の E1〜E12 が通る
- BLE の payload の契約テスト（`testdata/beacon/payload.json`、15 §5）が Go 側で通る

## S4 フレンド

| 内容 | 仕様 | 優先度 |
| --- | --- | --- |
| 招待コード、申請、承認・拒否・取消、一覧、削除 | 07 §2 | P0 |
| 通知の行を作る（`notifications` と outbox への積み込み）と `GET /notifications`。FCM での送信は S8 | 12 §1、§4 | P0 |

完了条件: 2 人の seed ユーザーで、招待コード経由と、すれ違い経由の両方でフレンドになれる。関係のない相手への申請が 403 になる。

## S5 instance 基盤

| 内容 | 仕様 | 優先度 |
| --- | --- | --- |
| `spaces`・`space_members`・`records` の model と、`InstanceStore`（SQLite 実装） | 03 §7.2、04 §2.4 | P0 |
| `AccessChecker`（判定結果のキャッシュを含む） | 10 §5 | P0 |
| 投稿の作成・一覧・削除（moderation は `moderation.Noop` を差しておく）、同意の確認 | 10 §4.1〜§4.3、02 §8 | P0 |
| メディアのアップロード、再エンコード、署名付き URL | 10 §4.4 | P0 |
| WebSocket の hub（認証、購読、再判定、イベント） | 10 §7 | P0 |
| `GET /instances` | 13 §2 | P0 |

seed のグループと archived の Party を使って、S6・S7 を待たずに確かめられる。

完了条件: seed のグループに 2 つの WebSocket 接続で購読し、片方の投稿がもう片方に `post.created` として届く。archived の Space に投稿すると 410 になる。画像の EXIF が落ちている。

## S6 Party

| 内容 | 仕様 | 優先度 |
| --- | --- | --- |
| `GET /party/friend-beacons`、`POST /party/presence`、近接エッジ | 08 §2、§3 | P0 |
| `party.Engine`（ワーカー）: 作成、確定、離脱、終了、統合 | 08 §4.1〜§4.3 | P0 |
| `GET /party/current`、退出、名前の変更 | 08 §6 | P0 |
| 写真の共有（`POST /instances/{id}/media`） | 08 §5、10 §4.4 | P0 |
| `GET /memories`（Party のみ） | 13 §2 | P0 |

完了条件: 15 §4.2 の P1〜P10 が通る。

## S7 グループ

| 内容 | 仕様 | 優先度 |
| --- | --- | --- |
| 作成（重複の抑制、`parent_space_id` の計算）、詳細、変更 | 09 §2、§9 | P0 |
| schedule の判定 | 09 §3 | P0 |
| アクセス判定（`AccessChecker` のグループ分） | 09 §4 | P0 |
| `GET /groups/nearby`、参加、退出、メンバー一覧 | 09 §5 | P0 |

完了条件: 15 §4.3 の G1〜G6 が通る。

## S8 moderation・通知送信

| 内容 | 仕様 | 優先度 |
| --- | --- | --- |
| NanoGPT クライアントと `moderation.Jev`（`Moderator` / `PostAssessor`） | 11 §5 | P0 |
| 投稿・グループ名・Party 名の同期判定と、`modq` の再判定ワーカー | 11 §3 | P0 |
| FCM の送信ワーカーと、端末の登録・削除 | 12 §2、§4 | P0 |

完了条件:
- `moderation.Fake` で block / warn / pending の 3 経路のテストが通る
- 保存した NanoGPT の応答（`testdata/nanogpt/`）でしきい値の計算のテストが通る
- 実機にフレンド申請の通知が届く

## S9 仕上げ（MVP）

| 内容 | 仕様 | 優先度 |
| --- | --- | --- |
| アカウント削除（`DELETE /me`） | 13 §5 | P0 |
| `cleanup.Janitor`（期限切れの token・招待・セッション・未添付メディアの掃除） | 04 §4 | P0 |
| 14 の P0 エンドポイントを一通り洗い、抜けを埋める | 14 | P0 |
| CI（`go vet`、`make lint`、`make test`、Redis 付きの `make test-integration`） | 15 §7 | P0 |

完了条件: 14 の P0 がすべて実装されている。seed ユーザーと実機 2 台で、すれ違い → フレンド → Party → 投稿・写真 → archive → memory の一連が通る。

## MVP の後（P1）

デモで見せたい順に並べる。順番は MVP の時点で見直す。

| 順 | 内容 | 仕様 | 状態 |
| --- | --- | --- | --- |
| 1 | すれ違いの集計（`/encounters/summary`）とダイジェスト通知、個人の統計 | 06 §6、12 §5、13 §3 | 済 |
| 2 | 公開投稿（Bluesky への同時投稿を含む）、Jetstream の購読と初回の読み込み、共通のフィード（`/feed?source=`） | 17、03 §4・§5 | 済 |
| 3 | closed グループの招待、永続化、エリア入場通知、グラフ API | 09 §6、§6.1、§7 | 済 |
| 4 | 自動 Pin・手動 Pin、Party の固定と確定の通知、返信、ステータス | 10 §3・§6、08 §4.4、12 §3、13 §1 | 済 |
| 5 | フレンドの位置共有（exact / approx / area / off）とエリア | 07 §4 | 済 |
| 6 | Jetstream で取り込んだ投稿の moderation | 11 §2 | 済 |
| 7 | 自動グループ（予定の送信、embedding、照合、自動参加）と投票 | 09 §8 | 済（実際の embedding サーバでの照合は未確認） |

| 8 | memory に archive したグループを出す、`DELETE /memories/{spaceId}` | 13 §2 | 済 |
| 9 | フレンドの位置共有の `direction` と `areasOnly` | 07 §4.1 | 済 |
| 10 | CHAIN からのプロフィール編集（名前・説明・アイコン・バナー） | 13 §1.3 | 済 |

## todo（P2）

draft の機能のうち、まだ実装していないもの。後回しにした L1〜L8（README）とあわせて、着手するときに仕様を詰める。

| # | 機能 | 内容 | 仕様 |
| --- | --- | --- | --- |
| T1 | すれ違った場所の記録 | 双方が許可したときだけ、geohash 6 桁を保存する（列はある） | 06 §7 |
| T2 | 本 party 同士の統合 | 今は統合しない。手動で統合する操作を足す | 08 §4.3、README Q6 |
| T3 | BLE でのグループ | GPS の代わりに、ゾーン内の在席メンバーを BLE で観測して判定する | 05 §1 |
| T4 | instance のリアクション | | 10 §3 |
| T5 | 公開投稿の画像 | PDS の blob に上げて、`jp.i32.chain.feed.post` と `app.bsky.feed.post` に付ける | 17 §2.1 |
| T6 | Bluesky で書いた投稿の取り込み | `app.bsky.feed.post` も Jetstream で取り込み、フィードに出すか | 17 §2.5 |
| T7 | 通報 | `POST /reports`、グループの owner と運営が見る一覧 | 11 §6 |
| T8 | 画像の moderation | 画像に対応したモデル（zero data retention のもの）で判定する | 11 §5.6 |
| T9 | 変わった表示 ID | draft「unique id は普遍的すぎる」 | README Q5 |
| T10 | 活発度、事業向けの集計 | 個人を特定できない粒度に限る | 13 §3、README Q7 |
| T11 | Spaces との統合 | `pds-spaces-alpha` の自前運用と `SpacesStore` の実装 | 03 §7.2、README Q4 |
| T12 | CDN に載らない PDS の画像 | `com.atproto.sync.getBlob` を backend で中継する | 13 §1.1 |
