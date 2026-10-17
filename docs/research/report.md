# CHAIN は純PWAで作れるか: すれ違い中核の可否と Android 推奨構成

対象は CHAIN 設計メモの機能を Android に置く場合の実現性である。
入力は `research/01` から `research/10` の10ファイルで、本レポートはそれ以外の事実を足さない。
参照した全 URL のアクセス日は 2026-10-16 である。
コードとPoCは扱わない。

## 0. 要旨 / 結論

結論から書く。

**純PWA（`navigator.bluetooth` だけを使うインストール型Webアプリ）では、CHAIN の中核である「すれ違い」を実装できない。**
決め手は3つある。

- advertise（周辺端末として advertise 信号を送信する役割）は仕様に存在しない。Web Bluetooth 仕様は central ロールのみを定義し、peripheral ロールは「not currently planned」と明言されている。
- passive scan（周辺の advertise 信号を受信する役割）はフラグ止まりである。`requestLEScan()` は `enable-experimental-web-platform-features` フラグを要し、Chromium 実装は「progress has stalled」で一般ユーザー向け本番には使えない。
- **バックグラウンドBLEはどのプラットフォームでも不可である。**「A PWA that runs a Bluetooth-connected service in the background ... is currently not possible on any platform」と明記される。加えて Web Bluetooth は Service Worker と Web Worker に公開されず、位置情報も `[Exposed=Window]` でページが無いと使えない。

すれ違いは advertise（emit）と passive scan（receive）の両方を要する（双方向で互いを認識した場合のみ成立）。
片方すら純PWA で本番運用できないのだから、中核は成立しない。

一方、**代替アーキテクチャは中核要件を満たせる。**
ネイティブ Kotlin は advertise（`BluetoothLeAdvertiser` と `BLUETOOTH_ADVERTISE`）と scan（`BluetoothLeScanner` と `BLUETOOTH_SCAN`）を同時に扱え、`connectedDevice` 型 FGS でバックグラウンド維持できる。
Capacitor は WebView シェルにネイティブ BLE プラグインと FGS を足す構成で中核を担え、Flutter は `flutter_ble_peripheral` で advertise を満たす。

それでも UI と SNS は Web で成立する。
12機能のうち、すれ違いと Party の在席判定、フレンドの常時位置共有を除く大半（instance、moderation、Pin、notification、profile、memory、統計、AT Protocol、グループの前面運用）は Web 層で実装できる。

推奨は1案に絞る。
**Next.js の static export を Capacitor シェルで包み、すれ違い中核をネイティブ Kotlin 実装と `connectedDevice` 型 FGS で担い、SNS 層を Web/WebView 側に置く。**

> [!WARNING]
> iOS は本レポートの対象外とする（本レポートが Android 専用である理由は第7節 U6 を参照）。
> Web Bluetooth は Safari で「Not supported and no plan to support it in the near future」であり、iOS 展開は別実装（別スタック）になる。

## 1. 検証の問いとスコープ

問いはひとつに絞れる。
CHAIN の各機能を Android のどの層に置けば、一次情報に照らして成立するか。
とくに中核の BLE すれ違いが、純PWA で成立するか。

スコープは Android と CHAIN の機能に限る。
iOS は一行のリスク注記に留める。
各主張は一次情報（公式仕様、公式ドキュメント、npm と pub のレジストリ、GitHub issue）に接地させ、出典のない断定はしない。

証拠の強さには差がある。
プラットフォーム仕様と API 可否は一次情報で確定できる。
OEM 実装差、ハードウェア差、審査運用、実測値、関係者合意は一次情報の外側にあり、確定事実として扱わず、実機で潰す対象として第7節に列挙する。

## 2. CHAIN 要件分解

CHAIN の設計メモは、すれ違いの成立条件をすでに明確に書いている。
「Advertiseでいい気がする255バイトまでらしい」と「advertiseを受信したときに成立」、そして「双方向で互いを認識した場合のみ」発生とみなす、という記述である。
つまりすれ違いは、自分が advertise 信号を送信する（emit）ことと、相手の advertise 信号を受信する（receive）ことの両方を要する。

設計メモが固定している前提は次のとおりである。

- すれ違いは双方向認識時のみ成立する。
- token 方式でユーザー id を秘匿化する（Backend が使い捨て token を発行）。
- 公開設定は即時 / 遅延 / 非公開の3種で、非対称は長い方の設定を適用する。
- Coded PHY（S=8）を長距離化に想定し、Advertise は 255 バイトまで。
- Party は 30 秒で仮 party、5 分で本 party とし、在席判定に継続 BLE を要する。
- グループは GPS を使う（教室では Bluetooth が厳しい）。
- グループは AT Protocol の Space 機能を前提とする。
- 統計はサーバに保存したい。
- Bluetooth と GPS を switch できる共通 interface を持つ。

機能は12行に分解される（11機能行 + AT Protocol の1行）。
各機能が要求するプラットフォーム能力を、要求マトリクスから要約する。

```
| 機能 | 主要な要求能力 | 設計メモの要点 |
|---|---|---|
| すれ違い機能 | BLE advertise(emit)、BLE passive scan(receive)、バックグラウンド検知(常時想定)、token はサーバ発行 | advertise 255B、双方向認識時のみ |
| フレンド機能 | 位置情報の常時共有(GPS)、geofence 相当のエリア指定 | 常時位置共有、指定エリアのみも可 |
| Party | BLE 在席判定(30秒仮→5分本)、端末カメラ、archive 保存 | Bluetooth 前提 |
| グループ | GPS(前面)、エリア入場通知、AT Protocol Space、サーバ relay の可能性 | 教室では BT 厳しい |
| instance | Web UI、in-app camera の要否は未確定、サーバ relay の可能性 | 投稿範囲は限定が妥当 |
| moderation | 外部 API(Jev)呼び出し、サーバ側テキスト検閲 | TypeSafeAI の Jev |
| Pin | サーバ側判定、UI | Jev 判定 |
| notification | プッシュ通知(フレンド申請、すれ違い数) | 集計元は BLE とサーバ |
| profile | Web UI、保存先は未確定(サーバ / AT Protocol 上か) | id、名前、アイコン等 |
| memory | archive party と写真の閲覧(保存先未確定) | サーバ / ローカル未確定 |
| 統計 | Web UI、サーバ保存希望 | サーバ保存希望 |
| AT Protocol | ブラウザからの外部プロトコル駆動 | AT Protocol Alpha |
```

主要な未確定事項も設計メモに列挙されている。
BLE と GPS のどちらで各機能を持つか、party とグループ内通信を BLE にするかサーバ relay にするか、archive の保存先、統計の保存先、位置情報の粒度である。
本レポートはこれらを確定させず、第5節の配置表と第7節の未検証点で扱う。

## 3. 純PWA 可否マトリクス（決め手の根拠つき）

問いはこう言い換えられる。
Android Chrome の純PWA で、BLE の3つのロールはどこまで届くか。

結論は3行で尽きる。

```
| role | API | Android Chrome での状態 | 出典 |
|---|---|---|---|
| central | navigator.bluetooth.requestDevice() | 可（フラグなし出荷） | implementation-status / MDN requestDevice / caniuse |
| scan | navigator.bluetooth.requestLEScan() | フラグ止まり（enable-experimental-web-platform-features 必須） | implementation-status / chromestatus |
| peripheral / advertise | 該当 API なし（仕様未定義） | 不可（仕様外・計画なし） | public-web-bluetooth-log 2024Apr |
```

central が出荷済みでも、すれ違いには使えない。
central は既知で許可済みのデバイスへの GATT 接続であり、不特定多数の周辺端末の自動発見には使えない。
発見には scan ロールが要り、scan はフラグ止まりである。

advertise に至っては API が存在しない。
仕様は central ロールのみを定義し、peripheral の挙動定義は「not currently planned」とされる。
`watchAdvertisements()` も「not planned to be shipped」であり、`getDevices()` とともにフラグの背後にある。

バックグラウンドはさらに厳しい。
Web Bluetooth は Service Worker と Web Worker に公開されないため、たとえ前面で scan が動いてもバックグラウンドでは動かない。
バックグラウンドBLE自体も「not possible on any platform」である。
位置情報も `[Exposed=Window]` であり、ページが無いと読めない。

純PWA で動くバックグラウンド手段は、イベント駆動で短命な Service Worker に限られる。
Web Push（FCM 経由、要可視通知）は届く。
Background Sync、Periodic Background Sync（12時間以上、インストール済みとエンゲージメントが条件）、Background Fetch は Chromium 限定である。
いずれも常時走るデーモンにはならない。

```
| 能力 | Android PWA での状態 | 主な出典 |
|---|---|---|
| Service Worker 寿命 | デーモンではない。イベントごとに起動し、アイドルで終了する | web.dev service-workers / MDN |
| Web Push | 可（FCM 経由、要可視通知） | MDN Push API / progressivewebapps.com |
| Background Sync | Chromium 限定（デーモンにはならない） | MDN Background Synchronization API |
| Periodic Background Sync | Chromium 限定、インストール済み、12時間以上 | progressivewebapps.com /periodic-sync |
| Background Fetch | Chromium 限定、大容量ダウンロード向け | MDN Background Fetch API |
| 位置情報 | バックグラウンド不可（[Exposed=Window]） | W3C geolocation / MDN |
| バックグラウンド BLE | 全PF不可 | whatwg/bluetooth #422 |
```

したがって純PWA の verdict は次のように確定する。
advertise = 不可、passive scan = 条件付き（フラグ）、BG BLE = 不可、バックグラウンド位置 = 不可。
中核のすれ違いは純PWA では成立しない。

## 4. 代替アーキテクチャ比較

純PWA が落ちたので、次に問うのは代替構成のどれが中核を満たすかである。
7つのアーキテクチャを13の評価軸で比較する（08 の比較マトリクスを要約するが、事実は保持する）。

```
| アーキテクチャ | advertise | passive scan | BG BLE | FGS | BG 位置 | Web資産(Next.js)再利用 | 開発コスト | 決め手の既知リスク |
|---|---|---|---|---|---|---|---|---|
| 純PWA | 不可 | 条件付き(フラグ) | 不可 | 不可 | 不可 | 可 | 中 | advertise/BG不可、scanはフラグ止まり |
| TWA | 不可 | 条件付き(フラグ) | 不可 | 不可 | 不可 | 可 | 低〜中 | 既定ブラウザ非ChromiumでCustom Tab化 |
| Capacitor | 別プラグインで可 | 可 | 要ネイティブ+FGS | 可 | 要ネイティブ | 可(static export必須) | 中 | community版はcentral専用、BGはネイティブ必須 |
| React Native | 条件付き | 可 | 不可〜条件付き | 要ネイティブ | 条件付き | 不可 | 中〜高 | peripheralライブラリ不健全(archived/404) |
| Flutter | 可 | 可 | 可 | 条件付き | 条件付き | 不可 | 中 | iOSはbluetooth-peripheralモード要 |
| ネイティブKotlin | 可 | 可 | 可 | 可 | 条件付き | 条件付き(WebView埋込) | 高 | BG開始制限、FGS type/permission必須 |
| Nearby Connections | 可 | 可 | 条件付き | 条件付き | 条件付き | 不可 | 中〜高 | Android専用、GMS必須、iOS非対応 |
```

決定的なのは BLE の列である。
advertise と scan と BG をフルに満たすのはネイティブ Kotlin だけである。
Flutter は advertise と scan と BG（Android はプロセス所有で継続）を満たす。
Capacitor は advertise を別プラグイン（`startAdvertising`）で、BG を FGS 内 BLE で満たすが、いずれもネイティブ実装が前提になる。
React Native は advertise ライブラリが不健全（`react-native-ble-advertiser` は archived、`react-native-ble-peripheral-manager` はソースリポジトリが 404）でリスクが高い。
Nearby Connections は Android 専用で GMS 必須であり、生の BLE advertise ではなく接続 API である。

Web資産の再利用は構成選択に効く。
Capacitor と純PWA と TWA は Next.js 資産を使えるが、React Native、Flutter、Nearby Connections は DOM を実行しないため使えない。

TWA は central こそ Chrome 由来で得るが、advertise と BG を満たさない。
Android WebView は `navigator.bluetooth` 自体を公開しない。

## 5. CHAIN 機能別の推奨配置

どの機能をどの層に置くか。
配置表に従い、12機能を4つの bucket（純PWAで可 / Capacitorで可 / ネイティブ必須 / 不可）に割り当てる。

```
| 機能 | 推奨技術 | 配置の根拠(08の該当セル) |
|---|---|---|
| すれ違い機能 | ネイティブ必須(BLE) + Capacitor(UI) | 純PWA: advertise不可/BG不可、Capacitor: advertise別プラグイン/BG要FGS、Kotlin: 可 |
| フレンド機能 | 純PWA(UI) + ネイティブ必須(常時位置) + サーバgeofence | 純PWA: BG位置不可(Exposed=Window) |
| Party | Capacitor(UI) + ネイティブ必須(BLE在席判定) | advertise不可/BG不可、継続BLEが要る |
| グループ | 純PWAで可(前面位置 + サーバgeofence) | BG位置不可=前面のみ可、プッシュ可、AT Protocol可 |
| instance | 純PWAで可(Web UI + サーバrelay) | Web資産可、カメラ・保存可、AT Protocol可 |
| moderation | 純PWAで可(fetch/BFF で外部API) | Jev 外部API呼び出し可 |
| Pin | 純PWAで可(サーバ側判定 + UI) | サーバ側判定 |
| notification | 純PWAで可(Web Push / FCM) | プッシュ通知可(要可視通知) |
| profile | 純PWAで可(Web UI + IndexedDB/サーバ) | ローカル保存可 |
| memory | 純PWAで可(IndexedDB/OPFS + サーバ) | 保存可、quota は best-effort |
| 統計 | 純PWAで可(Web UI + サーバ保存) | サーバ保存希望 |
| AT Protocol | 純PWAで可(ブラウザSPA、BFF推奨) | SPA実装可、BFF推奨 |
```

ネイティブ必須はすれ違いと Party の在席判定だけである。
理由は BLE advertise と BG BLE が Web で不可だからである。
フレンドの常時位置共有は位置成分がネイティブ必須で、前面のみの共有に限れば Web で可。
残りの Web 系（instance、moderation、Pin、notification、profile、memory、統計、AT Protocol）とグループの前面運用は Web 層で成立する。

## 6. Android 推奨アーキテクチャ

配置を1つの構成に組む。
Next.js の static export（`output: 'export'` の `out/`）を Capacitor シェルで包み、すれ違い中核をネイティブ Kotlin と `connectedDevice` 型 FGS で担い、SNS 層を Web/WebView 側に置く。

```
[Capacitor Android app]
  WebView 層 : Next.js static export (UI / SNS / AT Protocol SPA / Web Push)
  Native 層  : Kotlin Capacitor plugin
                BluetoothLeAdvertiser (BLUETOOTH_ADVERTISE)  ... すれ違い emit
                BluetoothLeScanner    (BLUETOOTH_SCAN)       ... すれ違い receive
                FGS type=connectedDevice                     ... BLE 接続を FGS 内で維持
```

層ごとに技術と根拠を対応づける。

```
| 層 | 採用技術 | 根拠 |
|---|---|---|
| 配布・アプリシェル | Capacitor(WebView + ローカル静的 HTTP サーバ) | 04 §6 |
| Web 資産 / UI | Next.js static export(SSR 不可) | Next.js static export / 09 |
| すれ違い advertise | ネイティブ Kotlin BluetoothLeAdvertiser(BLUETOOTH_ADVERTISE) | 06 §1 |
| すれ違い scan | ネイティブ Kotlin BluetoothLeScanner(BLUETOOTH_SCAN、必要なら neverForLocation) | 06 §1 |
| すれ違い BG 維持 | connectedDevice 型 FGS(BLE 接続を FGS 内で張る) | 04 §3.3 / 06 §1 |
| JS とネイティブの橋渡し | Capacitor ネイティブプラグイン(Kotlin) | 04 §3-4 |
| 常時位置共有(フレンド) | ネイティブ位置(前面のみなら Web 可) | 03 / 09 |
| SNS 層 AT Protocol | Web/WebView SPA(BFF 推奨) | 07 §2 |
| SNS 共通機能 | Web 層(fetch/BFF、IndexedDB/OPFS、KaTeX/marked) | 07 §1 |
| notification | Web Push(FCM 経由、要可視通知) | 03 / 09 |
| グループ | Web 層(前面位置 + サーバ geofence) | 09 |
```

この1案になる理由は3点に集約できる。
第一に、BLE を Web 層に置けないことが構成を決める（advertise と BG BLE が不可）。
第二に、advertise と scan と BG をフルに満たすのはネイティブ Kotlin だけである。
第三に、UI と SNS と AT Protocol は Web で成立するため Web 層を残す価値がある（AT Protocol はブラウザ SPA で駆動でき、BFF で public client の短いリフレッシュ寿命を緩和できる）。

Capacitor には注意が要る。
community 版 BLE プラグインは central 専用である。
advertise は別プラグイン（capgo の `startAdvertising`）で補う。
FGS は立てるだけでなく、BLE 接続そのものを FGS 内で張る必要がある（issue #643 の報告）。

TWA と WebView が選ばれない理由も明快である。
TWA は advertise と BG を満たさず、非 Chromium の既定ブラウザでは Custom Tab に落ちる。
WebView は `navigator.bluetooth` を公開しない。

## 7. 未検証リスク / 実機で潰す点

ここまでの結論はプラットフォーム仕様の層で確定している。
しかし推奨構成の前提を揺らし得る事項が、一次情報の外側に残る。
それらは確定事実ではなく、実機 PoC と審査と関係者合意で潰す対象である。

未検証点を7件挙げる。

- **U1 ベンダー別の BG BLE 継続**。`connectedDevice` 型 FGS で advertise を継続したとき、Samsung、Xiaomi、Oppo、Huawei、Pixel 等の省電力下で FGS が kill されず advertise が止まらないか。OEM 固有の省電力実装に公開仕様がなく端末依存である。
- **U2 Coded PHY と Extended Advertising の端末差**。legacy 31 バイトか Bluetooth 5 extended か、Coded PHY による長距離化を採るかが、対象端末群で送信側と受信側の両方で成立するか。ハードウェアと PHY の対応差に依存する。
- **U3 FGS の UX と Google Play 審査**。常駐 FGS の可視通知が UX として許容されるか、`connectedDevice` 型 FGS が審査を通るか。審査は案件ごとの判断である。
- **U4 電池消費**。advertise 常時と passive scan と FGS 常駐の同時稼働でのバッテリー持ちと発熱。実測が必要である。
- **U5 Nearby Connections を採る場合の実挙動**。接続戦略ごとの発見レイテンシ、成功率、BG 維持、電池。Android 専用で GMS 必須である点も前提を崩す。
- **U6「iOS は非対応」の関係者合意**。本推薦は Android 専用構成であり、iOS 展開は別実装になる。これは技術ではなく要件と体制の未確定事項である。
- **U7 WebView 内の Web Bluetooth シム経由の挙動**。`shimWebBluetooth` 経由ですれ違いの継続 scan と advertise がネイティブ直実装と同等に機能するかは一次情報にない。本推薦はシムに依存せずネイティブ直実装を採る。

extrinsic 制約（プラットフォーム側の要件であり、法的助言ではない）も4件挙げる。

- **E1 付近のデバイス権限審査**。`BLUETOOTH_SCAN` と `BLUETOOTH_ADVERTISE` と `BLUETOOTH_CONNECT` は Android 12 以降 runtime permission であり、審査区分に該当し得る。
- **E2 `neverForLocation` のトレードオフ**。位置権限を外せる代わりに「interact できる Bluetooth デバイスの種別が制限され得る」。
- **E3 位置情報の扱い**。常時位置はネイティブ必須で、権限とデータ保護の論点を伴う。
- **E4 すれ違い履歴の保存**。token はサーバ発行であり、保存先、保持期間、他端末への伝播はプライバシー設計の論点である。

coverage の限界も正直に記す。
次の参照は一次情報として採用できなかった。
`WebBluetoothCG/web-bluetooth` の issue #422（BG BLE 不可の根拠）は、リポジトリが `whatwg/bluetooth` へ移り、旧 URL は 404、正規 URL は 200 である（本レポートは正規 URL を採用）。
MDN の `requestLEScan` と `watchAdvertisements` のページは 404 であり、代わりに implementation-status と blink-dev intent を一次情報とした。
crbug `1043409` は「Access is denied to this issue」で引用不可、crbug `1018925` は「Invalid ID.」で存在しない。

## 8. 付録: 出典一覧

02（Web Bluetooth の3ロール）:

- https://developer.mozilla.org/en-US/docs/Web/API/Web_Bluetooth_API
- https://developer.mozilla.org/en-US/docs/Web/API/Bluetooth/requestDevice
- https://developer.mozilla.org/en-US/docs/Web/API/Bluetooth/getDevices
- https://caniuse.com/web-bluetooth
- https://github.com/WebBluetoothCG/web-bluetooth/blob/main/implementation-status.md
- https://chromestatus.com/feature/5346724402954240
- https://lists.w3.org/Archives/Public/public-web-bluetooth-log/2024Apr/0004.html
- https://lists.w3.org/Archives/Public/public-web-bluetooth-log/2025Oct/0007.html
- https://groups.google.com/a/chromium.org/g/blink-dev/c/lqCQ63CTKEQ
- https://bluetooth.spec.whatwg.org/

03（PWA バックグラウンド実行）:

- https://web.dev/learn/pwa/service-workers
- https://developer.mozilla.org/en-US/docs/Web/API/Service_Worker_API
- https://progressivewebapps.com/background/
- https://developer.mozilla.org/en-US/docs/Web/API/Background_Synchronization_API
- https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Guides/Offline_and_background_operation
- https://progressivewebapps.com/background/periodic-sync/
- https://developer.mozilla.org/en-US/docs/Web/API/Web_Periodic_Background_Synchronization_API
- https://developer.mozilla.org/en-US/docs/Web/API/Background_Fetch_API
- https://progressivewebapps.com/background/background-fetch/
- https://developer.mozilla.org/en-US/docs/Web/API/Push_API
- https://progressivewebapps.com/background/push-notifications/
- https://www.w3.org/TR/geolocation/
- https://developer.mozilla.org/en-US/docs/Web/API/Navigator/geolocation
- https://github.com/whatwg/bluetooth/issues/422

04（Capacitor）:

- https://github.com/capacitor-community/bluetooth-le
- https://capgo.app/docs/plugins/bluetooth-low-energy/
- https://registry.npmjs.org/@capgo/capacitor-bluetooth-low-energy
- https://github.com/capacitor-community/bluetooth-le/issues/643
- https://github.com/ionic-team/capacitor-background-runner/
- https://registry.npmjs.org/@capacitor/background-runner
- https://ionic.io/blog/how-capacitor-works-2
- https://ionicframework.com/docs/core-concepts/webview
- https://capacitorjs.com/docs/web
- https://nextjs.org/docs/app/guides/static-exports

05（React Native と Flutter）:

- https://registry.npmjs.org/react-native-ble-advertiser
- https://raw.githubusercontent.com/vitorpamplona/react-native-ble-advertiser/master/README.md
- https://registry.npmjs.org/react-native-ble-peripheral-manager
- https://raw.githubusercontent.com/dotintent/react-native-ble-plx/master/README.md
- https://registry.npmjs.org/react-native-ble-plx
- https://registry.npmjs.org/react-native-background-fetch
- https://raw.githubusercontent.com/juliansteenbakker/flutter_ble_peripheral/develop/README.md
- https://pub.dev/packages/flutter_ble_peripheral
- https://pub.dev/packages/flutter_ble_central
- https://pub.dev/packages/flutter_blue_plus
- https://pub.dev/packages/nearby_connections
- https://raw.githubusercontent.com/mannprerak2/nearby_connections/master/android/build.gradle
- https://developers.google.com/nearby/connections/strategies
- https://developers.google.com/nearby/connections/android/get-started

06（ネイティブ Kotlin と TWA）:

- https://source.android.com/docs/core/connect/bluetooth/ble
- https://developer.android.com/develop/connectivity/bluetooth/bt-permissions
- https://developer.android.com/develop/connectivity/bluetooth/ble/background
- https://developer.android.com/develop/background-work/services/fgs/service-types
- https://developer.android.com/develop/background-work/services/fgs/changes
- https://developer.android.com/reference/kotlin/android/bluetooth/le/BluetoothLeAdvertiser
- https://developer.android.com/reference/kotlin/android/bluetooth/le/BluetoothLeScanner
- https://raw.githubusercontent.com/whatwg/bluetooth/main/implementation-status.md
- https://developer.chrome.com/docs/android/trusted-web-activity/
- https://developer.chrome.com/docs/android/custom-tabs
- https://developer.chrome.com/docs/capabilities/bluetooth

07（SNS 共通機能と AT Protocol）:

- https://atproto.com/specs/oauth
- https://atproto.com/guides/about-oauth
- https://github.com/bluesky-social/atproto/tree/main/packages/oauth/oauth-client-browser
- https://web.dev/learn/pwa/capabilities
- https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps
- https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia
- https://developer.mozilla.org/en-US/docs/Web/API/IndexedDB_API
- https://developer.mozilla.org/en-US/docs/Web/API/File_System_API/Origin_private_file_system
- https://developer.mozilla.org/en-US/docs/Web/API/Notifications_API
- https://developer.mozilla.org/en-US/docs/Web/API/Storage_API/Storage_quotas_and_eviction_criteria
- https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API
- https://web.dev/learn/pwa/offline-data
- https://web.dev/learn/pwa/os-integration
- https://web.dev/articles/push-notifications-overview
- https://katex.org/docs/browser
- https://marked.js.org/

## 用語集

本レポートで使う主要な技術語を、本レポートでの用法に即して定義する。

- **PWA**：インストール可能な Web アプリ。Service Worker と Web App Manifest を備える。
- **純PWA**：`navigator.bluetooth` など Web 標準 API だけを使い、ネイティブ実装を足さないインストール型 Web アプリ。
- **BLE**：Bluetooth Low Energy。近距離無線の規格。
- **advertise**：周辺端末として advertise 信号を送信する BLE の役割。
- **central**：advertise を送信する周辺端末へ接続する側の BLE ロール。
- **peripheral**：advertise を送信する周辺端末側の BLE ロール。Web Bluetooth 仕様はこれを定義しない。
- **passive scan**：周辺端末の advertise 信号を受信する BLE の役割。`requestLEScan()` が担う。
- **GATT**：BLE で characteristic を読み書きするためのプロトコル。central が接続後に使う。
- **emit / receive**：advertise 信号を送信する側（emit）と受信する側（receive）。
- **FGS（Foreground Service）**：Android で可視通知を伴って常駐し続けるサービス。BLE 接続の維持に使う。
- **connectedDevice**：BLE 接続を維持する用途の FGS type。
- **TWA**：Trusted Web Activity。Chrome を再利用して Web アプリを全画面表示する Android の仕組み。
- **WebView**：アプリに埋め込まれたブラウザ表示領域。`navigator.bluetooth` を公開しない。
- **Service Worker**：ページとは別に動くイベント駆動のスクリプト。常駐デーモンにはならない。
- **Web Push**：FCM 経由でブラウザに届くプッシュ配信。可視通知を要する。
- **FCM（Firebase Cloud Messaging）**：Web Push の配送に使う Google の通知基盤。
- **BG（バックグラウンド）**：アプリが前面にない状態。表では BG と略す。
- **BFF**：Backend for Frontend。フロントエンド専用に置くサーバ層。
- **CORS**：ブラウザが別オリジンの資源取得を制御する仕組み。
- **AT Protocol**：Bluesky の分散 SNS プロトコル。本レポートでは Space 機能を前提とする。
- **SPA**：単一ページで動く Web アプリ。
- **static export**：Next.js がビルド時に静的ファイルを出力する機能（`output: 'export'`）。
- **PKCE**：OAuth の認可コード横取りを防ぐ拡張。
- **DPoP**：OAuth のアクセストークンを公開鍵に束縛する仕組み。
- **PAR**：Pushed Authorization Requests。認可要求をサーバ側で先に送る OAuth 拡張。
- **IndexedDB**：ブラウザの構造化オブジェクトストア。ローカル保存に使う。
- **OPFS**：Origin Private File System。ブラウザのオリジン専用ファイルシステム。
- **geofence**：ある地点を中心とした仮想の境界。エリア入場の判定に使う。
- **Jev**：moderation と Pin の判定に使う TypeSafeAI の外部 API。
- **Coded PHY**：BLE の長距離向け物理層（S=8 など）。
