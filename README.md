# CHAIN

[![IMAGE ALT TEXT HERE](https://jphacks.com/wp-content/uploads/2026/07/IMG_5316.png)](https://www.youtube.com/watch?v=piaNsc6ilBI)

**すぐ近くにいる人と、気軽につながる SNS。** Bluetooth が届く範囲の「となりの人」とのつながりを、Bluesky（AT Protocol）のアカウントの上につくります。

## 製品概要
### 背景(製品開発のきっかけ、課題等）
SNS で毎日目にするのは、遠い国のニュースや会ったこともない人の投稿です。一方で、同じ教室や同じイベント会場にいて、顔は知っているのに一度も話したことがない人がいます。近くにいるのに、つながる手段がありません。

いまの SNS は、全世界とつながる巨大なものか、すでに知り合いの人だけの閉じたものかのどちらかです。私たちは、**大きくも小さくもない、気軽に入れる中くらいの SNS** を作りたいと考えました。SNS が一番役に立つのは、遠くの出来事より身近な出来事のはずだからです。

### 製品説明（具体的な製品の説明）
CHAIN は Android アプリです。Bluesky のアカウントでログインし、スマートフォンの Bluetooth（BLE）と GPS で「いま、まわりに誰がいるか」を判定して、次の機能を提供します。

| 機能 | 内容 |
| --- | --- |
| ちかく | アプリを開くと、自分を中心に、近くの友達・近くにいる知らない人の数・近くのグループが泡のグラフで並びます。地図は使いません |
| すれ違い | BLE で互いを検出すると「すれ違い」が成立します。相手が誰かを見られるまでの時間（すぐ / 遅らせる / 見せない）を自分で選べます |
| 友達 | 友達＝Bluesky の相互フォロー。友達とは位置を「エリア名だけ」などの粒度で共有できます |
| Party | 友達と一緒にいると、30 秒で仮 Party、5 分で本 Party が自動で始まります。その間に撮った写真は自動で Party に共有され、離れると終わって memory に残ります |
| グループ | その場所にいる人だけが参加できるグループ。open（近くの人がリストから参加）と closed（招待制）を選べます。グループ同士の包含関係はグラフで表示します |
| 公開投稿 | 自分の PDS に投稿します。投稿ごとに Bluesky にも同時に出せます |
| memory・統計 | 終わった Party の写真と投稿、すれ違いの回数や友達の数を振り返れます |

### 特長
#### 1. 地図ではなく「グラフ」で、いまのまわりを表す
その場にいれば周りは見えるので、地図に投影する意味はありません。CHAIN は自分を中心に、近くの友達・知らない人・グループを大きさの違う泡として並べます（`ちかく` 画面）。グループも地図上の円ではなく、大小の包含関係のグラフとして扱います。

#### 2. Bluetooth で「本当に近くにいる」ことを確かめ、プライバシーは端末とサーバーで守る
BLE で配るのはユーザー ID ではなく、サーバーが発行する 15 分ごとに変わる使い捨ての token です。token から本人は割り出せず、すれ違いは**双方向に検出できたときだけ**成立します。相手が見えるまでの遅延は、2 人の設定のうち長い方を使います。位置や生のすれ違いデータは公開の場所に書かず、Redis の TTL で自然に消えます。

#### 3. Bluesky（AT Protocol）のアカウントとつながりをそのまま使う
新しいアカウントを作らず、Bluesky でログインします。プロフィールは `app.bsky.actor.profile`、友達は Bluesky の相互フォロー、公開投稿はユーザー自身の PDS に書くので、CHAIN で生まれたつながりは CHAIN の外にも持ち出せます。自分のデータがどこ（どの PDS、CHAIN のサーバー）に置かれているかを画面で確かめられます。

### 解決出来ること
- **近くにいるのに話せない**: すれ違いと「ちかく」画面で、同じ場所にいる人の存在に気づけ、グループで気軽に声をかけられます
- **思い出が散らばる**: 友達と一緒にいるだけで Party が始まり、写真が自動で集まります。終われば memory にまとまります
- **大きな SNS は疲れる、小さな SNS は入りにくい**: その場にいる人だけの、中くらいの空間を提供します
- **ロックイン**: アカウント・関係・公開投稿は Bluesky / ATProto の上にあるので、CHAIN をやめても失われません

### 今後の展望
- ATProto Spaces（限定公開のデータ）の正式版に合わせて、グループ / Party の投稿を Spaces に移す（いまは Spaces と同じ形のモデルで SQLite に保存しています）
- バックグラウンドでの Party の成立、iOS 対応
- Space（コミュニティ）ごとに moderation の方針を選べるようにする、外部ラベラーの購読
- データのエクスポート、すれ違った人の投稿を集めた Bluesky のカスタムフィード
- 予定の意味の近さ（embedding）から自然にできるグループ、投票による招待制への移行
- 電気通信事業法の届出を済ませたうえでの公開

### 注力したこと（こだわり等）
* **プライバシーを設計の中心に置いたこと。** 使い捨ての beacon token、双方向での成立、公開の遅延、位置共有の粒度（エリア名だけ等）、写真の EXIF の削除、Party のメンバーでない人に投稿や写真が漏れないための所属の不変条件などを、仕様書に書いてから実装しました
* **実機で止まらないこと。** BLE の常駐サービス、再起動後の復帰、権限の取り消し、端末とサーバーの時刻のずれなどを、Android の実機で落ちないように作り込みました
* **ATProto の考え方（credible exit、データを自分で持つ）に沿うこと。** 独自の関係表を作らず Bluesky のフォローを使い、lexicon を公開し、データの置き場所を見せています
* **仕様書から作ったこと。** `specification/` に 19 章の backend 仕様と Android の取り決めを書き、それに沿って実装・テストしています

## 開発技術
### 活用した技術
#### API・データ
* AT Protocol / Bluesky（OAuth + DPoP、PDS への書き込み、Jetstream、`app.bsky.*`）
* NanoGPT Decisions API（`typesafe/jev-1.13`）による投稿の moderation と自動 Pin
* Firebase Cloud Messaging（通知）

#### フレームワーク・ライブラリ・モジュール
* フロントエンド: Next.js 16（App Router、static export）、React 19、TypeScript
* モバイル: Capacitor 8 と Kotlin の native プラグイン（BLE、位置、写真、触覚）、Room、Android Keystore
* バックエンド: Go、Fiber v3（REST / WebSocket）、[indigo](https://github.com/bluesky-social/indigo)、SQLite（WAL）、Redis（GEO、TTL、pub/sub）
* テスト: Go の単体・結合テスト、Playwright、Puppeteer（WebView の E2E）、GitHub Actions
* インフラ: Cloudflare Tunnel

#### デバイス
* Android スマートフォン（BLE の advertise / scan、GPS、カメラ）

### 独自技術
#### ハッカソンで開発した独自機能・技術
* **BLE によるすれ違いと近接判定**: 15 分ごとに変わる使い捨て token を advertise / scan し、受信を Room に貯めて backend に報告する。token と時刻のずれは暗号化して保存し、再起動後も常駐サービスを復帰する
  * [web/android/app/src/main/java/jp/i32/chain/proximity/](web/android/app/src/main/java/jp/i32/chain/proximity/)、[web/src/device/proximity/](web/src/device/proximity/)
  * 8c226df（sighting の Room 保存）、f6e8fa1（token と時刻ずれの暗号化保存）、41d6809（時刻ずれの推定）、3ed2de4（再起動後の復帰）
* **BLE と GPS を切り替えられる近接判定の共通 interface**: すれ違い・Party（`PeerProximity`）とグループ（`ZonePresence`）を、BLE / GPS のどちらでも実装できる形に分けた
  * [specification/backend/05-proximity.md](specification/backend/05-proximity.md)、[backend/internal/service/proximity/](backend/internal/service/proximity/)
* **Party の自動成立**: 友達の近接グラフから 5 秒ごとに仮 Party → 本 Party → 終了を判定する
  * [backend/internal/service/party/](backend/internal/service/party/)、[web/src/device/proximity/party.ts](web/src/device/proximity/party.ts)
  * b302078（presence の報告）
* **「ちかく」画面の泡のグラフ**: 自分を中心に、友達・知らない人・グループを泡で配置する
  * [web/src/app/page.tsx](web/src/app/page.tsx)、120f448（BubbleCanvas）
* **ATProto との統合**: 友達＝相互フォロー、lexicon の検証と公開投稿の self-label、PDS とデータの置き場所の表示
  * [backend/lexicons/](backend/lexicons/)、52755f8（O5）、8c2c608（O2・O3）、08ca84d（O1・O4）

## リポジトリの構成

| パス | 内容 |
| --- | --- |
| [web/](web/) | Next.js のフロントエンドと Android アプリ（Capacitor）。起動方法は [web/README.md](web/README.md) |
| [backend/](backend/) | Go のバックエンド（`make` でテスト・起動） |
| [specification/](specification/) | backend の仕様書と Android native プラグインの取り決め |
| [docs/](docs/) | 企画の草案、技術調査、画面仕様、実装メモ |
