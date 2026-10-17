# CHAIN Backend 仕様

`docs/chain-draft.md` の機能案を、backend の実装単位まで具体化した仕様書群。
Android クライアント側の構成（Capacitor + Kotlin native BLE）は `docs/research/report.md` の推奨構成を前提とする。

## 文書一覧

| # | ファイル | 内容 |
| --- | --- | --- |
| 01 | [01-architecture.md](01-architecture.md) | 全体構成、プロセス、ディレクトリ構成、既存コードからの変更点、開発環境（Tunnel） |
| 02 | [02-conventions.md](02-conventions.md) | API 規約、ID・時刻、エラー、認可、レート制限、設定値 |
| 03 | [03-auth-atproto.md](03-auth-atproto.md) | ATProto OAuth（BFF）、アプリセッション、PDS 書き込み、Jetstream、lexicon、Spaces 整合 |
| 04 | [04-data-model.md](04-data-model.md) | SQLite スキーマ、Redis キー設計、保持期間 |
| 05 | [05-proximity.md](05-proximity.md) | BLE / GPS を切り替える近接判定の共通 interface、beacon token |
| 06 | [06-encounter.md](06-encounter.md) | すれ違い |
| 07 | [07-friend-location.md](07-friend-location.md) | フレンド、位置共有 |
| 08 | [08-party.md](08-party.md) | Party |
| 09 | [09-group.md](09-group.md) | グループ |
| 10 | [10-instance.md](10-instance.md) | instance（グループ / Party 内の投稿）、メディア、Pin、WebSocket |
| 11 | [11-moderation.md](11-moderation.md) | moderation（Jev / NanoGPT Decisions API）、通信の秘密への配慮 |
| 12 | [12-notification.md](12-notification.md) | 通知 |
| 13 | [13-profile-memory-stats.md](13-profile-memory-stats.md) | profile、memory、統計、ブロック、アカウント削除 |
| 14 | [14-api.md](14-api.md) | REST / WebSocket エンドポイント一覧 |
| 15 | [15-testing.md](15-testing.md) | テスト方針 |
| 16 | [16-implementation-order.md](16-implementation-order.md) | 実装の順番（S0〜S9 と P1） |
| 17 | [17-feed.md](17-feed.md) | 公開投稿（Bluesky への同時投稿）と、共通のフィード |
| 18 | [18-code-map.md](18-code-map.md) | 機能とコードのパス、コントローラーとパスの一覧（`make codemap` で生成） |
| 19 | [19-openness.md](19-openness.md) | ATProto の特長を活かす拡張: Space ごとの moderation 方針、データのエクスポート、Spaces 統合の段階、Bluesky へのつながりの持ち出し、lexicon の公開、データの置き場所 |
| — | [../android/README.md](../android/README.md) | Android native プラグインと backend の取り決め |

## 確定した方針

| 論点 | 決定 | 理由 |
| --- | --- | --- |
| HTTP フレームワーク | Fiber v3（既存） | draft の Tech 表どおり |
| DB | SQLite（永続）＋ Redis（揮発・位置・近接・pub/sub・レート制限） | draft どおり。位置とすれ違いの生データは Redis の TTL で自然消滅させる |
| ATProto 連携 | Bluesky OAuth でログインし、backend は **BFF 兼 AppView** として動く | アカウントとプロフィールを既存の ATProto 資産に乗せる |
| 公開データ | 公開投稿・CHAIN 公開プロフィールは **ユーザーの PDS** に書く | ATProto の通常の repo は全世界公開 |
| 限定データ（instance） | グループ / Party を **ATProto Spaces と同形のモデル**で SQLite に保存する。Spaces 正式版の後に `InstanceStore` の実装を差し替えて統合する | Spaces は 2026-08 時点で alpha（本番利用不可・Go SDK なし・bsky.social の対応未確認）。形を揃えておけば移行はデータ移送で済む |
| 個人データ | すれ違い・位置・フレンド・設定は SQLite / Redis のみ。Spaces にも載せない | サーバが判定に使う個人データであり、共有空間のデータではない |
| instance 内通信 | サーバ relay（WebSocket）。BLE は在席判定とメンバー検出にだけ使う | 写真共有と同時接続数の制約を避ける |
| NSID | `jp.i32.chain.*`（ドメイン `chain.i32.jp` の逆順） | 所有ドメイン |
| API ホスト | `https://chain.i32.jp`（Cloudflare Tunnel → `localhost:8080`、01 §6） | Cloudflare の無料証明書は 1 階層下（`*.i32.jp`）までしか覆わないため、`api.chain.i32.jp` ではなく `chain.i32.jp` を使う |
| DB の変更 | migration を使わず、毎回作り直して seed を流す（04 §5） | 開発初期でスキーマが頻繁に変わるため |
| 電気通信事業法 | グループ / Party 内の投稿は**届出対象として扱い**、公開前に届出る。通信の秘密を前提に設計する（02 §8） | 特定の相手との間の通信の媒介に当たるため |
| DM | MVP では実装しない。届出後に再検討できる | 届出をしても、DM には通報・moderation の設計が別途要る |
| moderation / Pin の判定 | NanoGPT Decisions API の `typesafe/jev-1.13` | 11 §5 |
| 自動グループ | 予定のタイトルの embedding（EmbeddingGemma など）の類似度と時間の重なりで判定し、条件を満たした人を自動で参加させる（09 §8） | 単語の一致より意味の近さで比べた方が精度が出るため |
| 公開投稿 | `jp.i32.chain.feed.post` を PDS に書く。投稿ごとに Bluesky（`app.bsky.feed.post`）にも出せる（17 §2） | |
| フィード | source を差し替えられる 1 つの API と要素の形（17 §3） | 画面ごとに別の API を作らず、あとから source を足せるようにするため |
| 対象 OS | Android のみ | `docs/research/report.md` U6 |
| 地図表示 | 使わない。グループは包含関係のグラフとして返す | draft「地図を用いる優位性はない」 |

## 優先度（MVP 線）

各仕様書の機能単位に以下のラベルを付ける。

- **P0**: MVP 必須。デモの主動線
- **P1**: MVP に余裕があれば入れる
- **P2**: 後回し

| 機能 | P0 | P1 | P2 |
| --- | --- | --- | --- |
| 認証 | ATProto OAuth ログイン、アプリセッション | — | 端末ごとのセッション管理 UI |
| profile | ATProto プロフィールの取得・キャッシュ | CHAIN ステータス | 変わった表示 ID（Q5） |
| すれ違い | beacon token 発行、sighting 報告、双方向成立、公開遅延、一覧 | すれ違い回数の集計 API | すれ違い場所の記録 |
| フレンド | 招待コード（QR）とすれ違い経由の申請・承認・削除 | 位置共有（exact / area / off） | direction モード、エリア限定共有 |
| Party | フレンド近接検出、仮 party → 本 party、投稿・写真、archive | 固定（lock） | 本 party 同士の手動統合 |
| グループ | 明示的 open グループ、geofence 内参加、nearby 一覧 | closed グループ（招待 quorum）、永続化、エリア入場通知、グラフ API | 自然発生グループ、投票 |
| instance | テキスト投稿、画像、WebSocket 配信 | markdown / TeX、返信 | リアクション |
| 公開投稿（PDS） | — | `jp.i32.chain.feed.post` の作成と Jetstream 経由の索引、ホームフィード | PDS への画像 blob |
| Spaces 統合 | Spaces 同形のデータモデル（URI、メンバー権限、アクセス判定関数） | — | `pds-spaces-alpha` 自前運用と `SpacesStore` 実装（03 §7） |
| moderation | 投稿前の block 判定 | warn 判定と表示設定、Jetstream 取り込み分の判定 | 通報 |
| Pin | — | Jev による自動 Pin | — |
| 通知 | フレンド申請 | Party 開始、グループ入場、すれ違いダイジェスト | 通知種別ごとの細かい設定 |
| memory | archive 済み Party の閲覧 | archive 済みグループ | — |
| 統計 | — | 個人の統計（すれ違い数、フレンド数） | 活発度、事業向け集計 |

## 未決事項

| # | 論点 | 現在の仮置き | 決める人 / 方法 |
| --- | --- | --- | --- |
| Q4 | Spaces 正式版の仕様差分と bsky.social の対応有無 | alpha の proposal（0016-permissioned-data）に合わせる | 正式版リリース時に 03 §7 を再確認 |
| Q5 | ユーザー ID の扱い（draft「unique id は普遍的すぎる」） | 内部 ID は DID、表示は ATProto handle | UI 設計時に再検討 |
| Q6 | 本 party 同士が出会ったときの統合 | 自動統合しない（08-party.md） | 実機で試して判断 |
| Q7 | 事業向け統計の範囲と同意取得 | 個人単位の統計のみ実装し、事業向け集計は P2 | プライバシーポリシー策定時 |
| Q8 | space authority の DID 方式 | `did:web:{API_HOST}`（03 §7.2） | Spaces 統合時に did:plc へ移すか判断 |
| Q9 | 投稿内容を外部 API（NanoGPT）で自動判定することへの同意の取り方 | 利用規約とは別に、初回起動時に個別の同意を取る（02 §8） | 届出の準備と合わせて確認（本仕様は法的助言ではない） |
| Q10 | Jev の日本語に対する判定精度と、しきい値 | 11 §5.3 の仮のしきい値 | 日本語の評価用データで実測して調整 |

## 解決済み

| # | 論点 | 決定 |
| --- | --- | --- |
| Q1 | lexicon の NSID authority | `jp.i32.chain`（ドメイン `chain.i32.jp`） |
| Q2 | グループ / Party 内投稿が電気通信事業法の届出対象になるか | 対象になる。公開前に届出る |
| Q3 | Jev の API | NanoGPT Decisions API（`POST https://api.nano-gpt.com/api/v1/decisions`） |

## 後でやること

決定を後回しにした項目。着手するときに該当する仕様書へ書き足す。

| # | 項目 | 内容 |
| --- | --- | --- |
| L1 | 開発用 API | sighting・近接・位置を偽装して入れる API、seed ユーザーでログインする手段。`CHAIN_ENV=dev` のときだけ有効にする |
| L2 | ストア審査と権限 | バックグラウンド位置（フレンドの常時位置共有）は Google Play の審査が重い。MVP では前面のみの共有にする案。BLE 常駐の FGS も審査の論点（research U3） |
| L3 | BLE の company ID | 開発用の `0xFFFF` を、本番では Bluetooth SIG の割り当てに替える（05 §2.3） |
| L4 | 個人情報保護法 | NanoGPT への送信が外国にある第三者への提供に当たるかの確認。`typesafe/jev-1.13` の zero data retention 対応の確認。Q9 と合わせる |
| L5 | 安全面と年齢 | つきまとい対策。年齢制限（例: 18 歳未満は位置共有なし）、位置共有をすぐ止められる UI |
| L6 | バックアップと再起動 | SQLite のバックアップ（Litestream など）、Redis の永続化方針、再起動時に Party の状態を SQLite から組み直す処理 |
| L7 | NanoGPT の費用と上限 | 利用者数 × 1 日の投稿数での費用見積もり。レート制限が非公開なので実測する |
| L8 | 予定のタイトルの扱い | 自動グループ（09 §8）のために、予定のタイトルと時間を平文で送り、サーバに保存する。スケールしたら、端末側での embedding 計算、保存期間の短縮、暗号化などを検討する。外部の embedding サービスに送る場合は L4 と同じく第三者提供の確認が要る |
