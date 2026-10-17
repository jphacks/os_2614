# 08 — CHAIN アーキテクチャ比較マトリクス（7 アーキテクチャ × 13 評価軸）

一次情報源は `01-requirements.md` 〜 `07-sns-atproto.md` のみ。本ファイルは新規の一次情報を追加せず、
02–07 が引用した URL だけを `## 出典` の**トークン（URL token）**として参照する。各セルは必ず
**可否 verdict + 出典トークン**を持ち、空セル・出典なしセルは存在しない。

- アクセス日（02–07 準拠）: **2026-10-16**
- 本マトリクスは todo 9（機能配置表）・todo 10（アーキテクチャ推薦）の入力。

## 記号（verdict）

| verdict | 意味 |
| --- | --- |
| 可 | 02–07 の一次情報で実現可能と確認できる |
| 不可 | 02–07 の一次情報で実現不可と確認できる |
| 条件付き | フラグ・権限・前提・別プラグイン等の条件付きで可（02–07 準拠） |
| 要native | JS/Web 層だけでは不可で、ネイティブ実装（Service/FGS 等）が必須 |
| 中 / 中〜高 / 高 | 開発コストの相対評価（02–07 の成熟度・必要実装量からの導出） |

> 注（証跡の性質）: BLE に関わる列（advertise / passive scan / バックグラウンドBLE / FGS / 既知リスク）は
> すべて 02–07 の BLE 一次情報に直接接地する。カメラ・保存 / バックグラウンド位置 / Web 資産再利用など
> ネイティブ系スタックの一部セルは、02–07 が引用する最も近い出典トークン（Web 能力・Android バックグラウンド
> 制約・Next.js static export 等）を付す。いずれも 02–07 の記載と矛盾しない範囲に留める。

## 比較マトリクス

| アーキテクチャ | BLE advertise可否 | BLE passive scan可否 | バックグラウンドBLE | 前面サービス(FGS) | バックグラウンド位置 | プッシュ通知 | カメラ・保存 | Web資産(Next.js)再利用 | 配布 | AT Protocol | 開発コスト | 既知リスク | 出典 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 純PWA | 不可（出典: S1,S6） | 条件付き（flag必須／central=可）（出典: S1,S3,S5,S7） | 不可（出典: S23,S2） | 不可（SWはevent-driven・FGS概念なし）（出典: S10,S12） | 不可（Exposed=Window）（出典: S21,S22） | 可（FCM・要可視通知）（出典: S19,S20） | 可（getUserMedia／IndexedDB・OPFS・quota）（出典: S56,S58,S61） | 可（static export成果物をそのまま配信）（出典: S29,S57） | 可（インストール型PWA・Web配布）（出典: S15,S57） | 可（SPA・BFF推奨）（出典: S66,S67,S68） | 中（BLE要件はflag/native代替が必須）（出典: S1,S23） | advertise不可・BG BLE不可・scanはflag止まり（出典: S1,S6,S23） | 網羅可（出典: S1,S2,S3,S5,S6,S7,S10,S12,S15,S19,S20,S21,S22,S23,S29,S56,S57,S58,S61,S66,S67,S68） |
| TWA | 不可（Web Bluetooth仕様にperipheralなし）（出典: S6,S55） | 条件付き（flag止まり／central=可）（出典: S1,S52,S55） | 不可（native BLE面なし・要前面/ユーザ操作）（出典: S6,S53） | 不可（Custom Tabs系・native serviceなし）（出典: S53,S54） | 不可（ブラウザweb platformのGeolocation制約）（出典: S21,S22） | 可（FCM）（出典: S19,S20） | 可（getUserMedia／IndexedDB）（出典: S56,S58） | 可（ブラウザがそのまま描画）（出典: S53,S54） | 可（Play Store・TWA）（出典: S53） | 可（SPA・BFF推奨）（出典: S67,S68） | 低〜中（Web資産流用・BLE制約）（出典: S53,S6） | advertise/BG不可・既定ブラウザ非ChromiumでCustom Tab化（出典: S53,S54） | 網羅可（出典: S1,S6,S19,S20,S21,S22,S52,S53,S54,S55,S56,S58,S67,S68） |
| Capacitor | 別プラグインで可（community=central専用／capgo=startAdvertising）（出典: S24,S25） | 可（requestLEScan／allowExtendedAdvertising）（出典: S24） | 要native+FGS（FGS内でBLE接続を張る）（出典: S26） | 可（capgo startForegroundService／Android only）（出典: S25,S26,S48） | 要native（常時BG不可・background-runnerにGeolocation）（出典: S27,S57） | 可（FCM）（出典: S19,S20） | 可（WebView getUserMedia／IndexedDB）（出典: S56,S58） | 可（static export必須・SSR不可）（出典: S28,S29,S30） | 可（iOS/Android native app）（出典: S28,S30） | 可（SPA・BFF推奨）（出典: S66,S67,S68） | 中（BG BLEはnative Service実装必須）（出典: S24,S26） | community版central専用・BG要native Service・background-runner BLE非対応（出典: S24,S26,S27） | 網羅可（出典: S19,S20,S24,S25,S26,S27,S28,S29,S30,S48,S56,S57,S58,S66,S67,S68） |
| React Native | 条件付き（advertiser=archived／peripheral-manager=repo 404）（出典: S31,S32,S33） | 可（ble-plx=central成熟）（出典: S34,S35） | 不可〜条件付き（BG service未実装TODO／plxはiOSのみBG）（出典: S32,S34,S36） | 要native（ライブラリ非提供）（出典: S32,S34） | 条件付き（background-fetchがBG機構）（出典: S36） | 可（FCM）（出典: S19,S20） | 可（native/JSモジュール）（出典: S57,S58） | 不可（RNはDOM非実行・Next.js成果物流用不可）（出典: S29） | 可（iOS/Android native app）（出典: S28,S30） | 条件付き（ブラウザSPA向けclient前提・別途対応要）（出典: S67,S68） | 中〜高（advertiseライブラリ不健全）（出典: S31,S32,S33） | peripheralライブラリ不健全（archived/404）・BG TODO（出典: S31,S32,S33） | 網羅可（出典: S19,S20,S28,S29,S30,S31,S32,S33,S34,S35,S36,S57,S58,S67,S68） |
| Flutter | 可（flutter_ble_peripheral／peripheral mode）（出典: S37,S38） | 可（flutter_ble_central／flutter_blue_plus）（出典: S39,S40） | 可（Android: process所有で継続／iOSはbluetooth-peripheral BG mode）（出典: S37,S38） | 条件付き（Androidはプロセス所有で継続・明示FGSは別途native）（出典: S37,S48） | 条件付き（native実装要）（出典: S22,S57） | 可（FCM）（出典: S19,S20） | 可（nativeモジュール）（出典: S57,S58） | 不可（独自レンダラ・DOM流用不可）（出典: S29） | 可（iOS/Android native app）（出典: S28,S30） | 条件付き（Dart実装/別途・ブラウザSPA client前提）（出典: S67,S68） | 中（peripheralライブラリ健全・別スタック）（出典: S37,S38） | iOS BGはbluetooth-peripheralモード要・プロセスkillで広告停止（出典: S37,S38） | 網羅可（出典: S19,S20,S22,S28,S29,S30,S37,S38,S39,S40,S48,S57,S58,S67,S68） |
| ネイティブKotlin | 可（BluetoothLeAdvertiser＋BLUETOOTH_ADVERTISE）（出典: S50,S46） | 可（BluetoothLeScanner＋BLUETOOTH_SCAN/neverForLocation）（出典: S51,S46） | 可（connectedDevice型FGS／PendingIntent scan）（出典: S47,S48） | 可（foregroundServiceType=connectedDevice・Android14でtype宣言必須）（出典: S48,S49） | 条件付き（native実装・Android BG実行制約）（出典: S47,S49） | 可（FCM）（出典: S19,S20） | 可（native OS API）（出典: S57,S58） | 条件付き（WebView埋込で静的資産可／navigator.bluetoothはWebView非公開）（出典: S30,S52） | 可（Play Store native app）（出典: S28,S30） | 条件付き（native HTTP＋DPoP/PKCE実装要・BFF推奨）（出典: S66,S67） | 高（BLE両ロール+BGをフル実装・最も確実）（出典: S47,S50,S51） | Android12+ BG開始制限・Android14 FGS type/permission必須・権限trio（出典: S46,S48,S49） | 網羅可（出典: S19,S20,S28,S30,S46,S47,S48,S49,S50,S51,S52,S57,S58,S66,S67） |
| Nearby Connections | 可（advertise・Android専用）（出典: S41,S43） | 可（discover・Android専用）（出典: S41,S43） | 条件付き（接続ライフサイクル依存・生BLE広告ではない）（出典: S41,S43） | 条件付き（接続維持は別途native/GMS）（出典: S43,S48） | 条件付き（native実装・GMS依存）（出典: S44,S49） | 可（FCM）（出典: S19,S20） | 可（native OS API）（出典: S57,S58） | 不可（GMS native API・DOMなし）（出典: S29,S43） | 可（GMS要求・Play Store）（出典: S28,S44） | 条件付き（native HTTP＋別途実装・BFF推奨）（出典: S66,S67） | 中〜高（GMS依存・Android専用）（出典: S41,S44） | Android-only・GMS必須・iOS非対応（出典: S41,S43,S44） | 網羅可（出典: S19,S20,S28,S29,S41,S42,S43,S44,S48,S49,S57,S58,S66,S67） |

セル数: 7 行 × 13 列 = **91 データセル**（+ 先頭のアーキテクチャ列 7 セル）。全 91 セルが
`verdict（出典: S…）` 形式で、空セルゼロ・出典なしセルゼロ。

## 列ごとの要点（02–07 との整合）

- **BLE advertise可否**: 純PWA=不可（S1,S6）／TWA=不可（S6,S55）／Capacitor=別プラグインで可（S24,S25）／RN=条件付き（S31,S32,S33）／Flutter=可（S37,S38）／Kotlin=可（S50,S46）／Nearby=可・Android専用（S41,S43）。
- **BLE passive scan可否**: 純PWA・TWA=条件付き（flag止まり、centralは可）（S1,S3,S5,S7,S52,S55）／Capacitor=可（S24）／RN=可（ble-plx）（S34,S35）／Flutter=可（S39,S40）／Kotlin=可（S51,S46）／Nearby=可（S41,S43）。
- **バックグラウンドBLE**: 純PWA=不可（S23,S2）／TWA=不可（S6,S53）／Capacitor=要native+FGS（S26）／RN=不可〜条件付き（S32,S34,S36）／Flutter=可（S37,S38）／Kotlin=可（S47,S48）／Nearby=条件付き（S41,S43）。
- **前面サービス(FGS)**: Capacitor=可（capgo・S25,S26,S48）／Kotlin=可（connectedDevice・S48,S49）／Nearby=条件付き（S43,S48）／Web系=不可（S10,S12,S53,S54）。
- 純PWA は advertise=不可・scan=flag・BG BLE=不可で 02・03 と一致。Capacitor は central=可／advertise=別プラグイン／BG=要native+FGS で 04 と一致。Kotlin は advertise+scan+BG(FGS)=可 で 06 と一致。TWA は central=可・advertise/BG=不可・WebView は `navigator.bluetooth` 非公開で 06 と一致。RN は advertiser archived／peripheral-manager repo 404 で 05 と一致。Flutter は `flutter_ble_peripheral` 可で 05 と一致。Nearby Connections は Android-only で 05 と一致。

## 出典

各トークンは 02–07 が引用した URL。本ファイルは新規一次情報を追加していない。

- **S1** — https://github.com/WebBluetoothCG/web-bluetooth/blob/main/implementation-status.md （02 / 06: central出荷・scan flag・getDevices/watchAdvertisements flag）
- **S2** — https://developer.mozilla.org/en-US/docs/Web/API/Web_Bluetooth_API （02: central概要 / Workers非対応）
- **S3** — https://developer.mozilla.org/en-US/docs/Web/API/Bluetooth/requestDevice （02: central=可）
- **S5** — https://chromestatus.com/feature/5346724402954240 （02: requestLEScan=flag止まり）
- **S6** — https://lists.w3.org/Archives/Public/public-web-bluetooth-log/2024Apr/0004.html （02: peripheralロール未計画=advertise不可）
- **S7** — https://lists.w3.org/Archives/Public/public-web-bluetooth-log/2025Oct/0007.html （02: scanの実装停滞）
- **S10** — https://web.dev/learn/pwa/service-workers （03: SWはevent-driven / 短命）
- **S12** — https://progressivewebapps.com/background/ （03: SWはdaemonではない / AndroidはFCMでpush起動）
- **S15** — https://progressivewebapps.com/background/periodic-sync/ （03: installed PWA・>=12h）
- **S19** — https://developer.mozilla.org/en-US/docs/Web/API/Push_API （03 / 07: Push API・前景外でも受信可）
- **S20** — https://progressivewebapps.com/background/push-notifications/ （03: FCM経由・要可視通知）
- **S21** — https://www.w3.org/TR/geolocation/ （03: Geolocationは`[Exposed=Window]`）
- **S22** — https://developer.mozilla.org/en-US/docs/Web/API/Navigator/geolocation （03: Navigator上のWindow限定API）
- **S23** — https://github.com/whatwg/bluetooth/issues/422 （03: BGのBluetooth接続サービスは全PFで不可）
- **S24** — https://github.com/capacitor-community/bluetooth-le （04: central専用 / requestLEScan 可 / advertise は別プラグイン）
- **S25** — https://capgo.app/docs/plugins/bluetooth-low-energy/ （04: startAdvertising / startForegroundService）
- **S26** — https://github.com/capacitor-community/bluetooth-le/issues/643 （04: BGはnative Service・FGSにBLE接続を移す必要）
- **S27** — https://github.com/ionic-team/capacitor-background-runner/ （04: WebView外JS環境 / BLE非対応 / 常時BG不可）
- **S28** — https://ionic.io/blog/how-capacitor-works-2 （04: WebViewベース / iOS・Android・PWAパッケージ）
- **S29** — https://nextjs.org/docs/app/guides/static-exports （04: `output:'export'`必須 / Nodeサーバ機能は不可）
- **S30** — https://ionicframework.com/docs/core-concepts/webview （04: iOS WKWebView / Android WebView）
- **S31** — https://registry.npmjs.org/react-native-ble-advertiser （05: advertiser=0.0.17 / 2022-09-27）
- **S32** — https://raw.githubusercontent.com/vitorpamplona/react-native-ble-advertiser/master/README.md （05: early development / BG service unchecked TODO / repo archived）
- **S33** — https://registry.npmjs.org/react-native-ble-peripheral-manager （05: 0.3.0 / GitHub repo 404）
- **S34** — https://raw.githubusercontent.com/dotintent/react-native-ble-plx/master/README.md （05: peripheral非対応 / centralのみ）
- **S35** — https://registry.npmjs.org/react-native-ble-plx （05: 3.5.1 / active）
- **S36** — https://registry.npmjs.org/react-native-background-fetch （05: 4.4.2 / BG機構）
- **S37** — https://raw.githubusercontent.com/juliansteenbakker/flutter_ble_peripheral/develop/README.md （05: Androidはadvertisementがprocess所有でBG継続 / iOSはbluetooth-peripheral BG mode）
- **S38** — https://pub.dev/packages/flutter_ble_peripheral （05: 3.1.0 / Android API 21）
- **S39** — https://pub.dev/packages/flutter_ble_central （05: central-mode scanning）
- **S40** — https://pub.dev/packages/flutter_blue_plus （05: central alternative）
- **S41** — https://pub.dev/packages/nearby_connections （05: Android-only / advertise+discover）
- **S42** — https://raw.githubusercontent.com/mannprerak2/nearby_connections/master/android/build.gradle （05: minSdkVersion 19 / play-services-nearby）
- **S43** — https://developers.google.com/nearby/connections/strategies （05: Cluster/Star/Point-to-Point）
- **S44** — https://developers.google.com/nearby/connections/android/get-started （05: Google Play Services SDK必須）
- **S46** — https://developer.android.com/develop/connectivity/bluetooth/bt-permissions （06: BLUETOOTH_ADVERTISE/SCAN/CONNECT・neverForLocation）
- **S47** — https://developer.android.com/develop/connectivity/bluetooth/ble/background （06: PendingIntent scan / 背景起動制約）
- **S48** — https://developer.android.com/develop/background-work/services/fgs/service-types （06: FGS type=connectedDevice）
- **S49** — https://developer.android.com/develop/background-work/services/fgs/changes （06: Android14 type宣言・Android12 BG開始制限）
- **S50** — https://developer.android.com/reference/kotlin/android/bluetooth/le/BluetoothLeAdvertiser （06: advertise API）
- **S51** — https://developer.android.com/reference/kotlin/android/bluetooth/le/BluetoothLeScanner （06: scan API）
- **S52** — https://raw.githubusercontent.com/whatwg/bluetooth/main/implementation-status.md （06: Android WebViewは`navigator.bluetooth`非公開）
- **S53** — https://developer.chrome.com/docs/android/trusted-web-activity/ （06: TWAはブラウザが描画 / central可・advertise/BG不可）
- **S54** — https://developer.chrome.com/docs/android/custom-tabs （06: TWAはCustom Tabs拡張 / 既定ブラウザのエンジン）
- **S55** — https://developer.chrome.com/docs/capabilities/bluetooth （06: Web BluetoothはCentralロールのみ）
- **S56** — https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia （07: camera・secure context必須）
- **S57** — https://web.dev/learn/pwa/capabilities （07: camera / IndexedDB / Web Push のPWA能力）
- **S58** — https://developer.mozilla.org/en-US/docs/Web/API/IndexedDB_API （07: local storage）
- **S61** — https://developer.mozilla.org/en-US/docs/Web/API/Storage_API/Storage_quotas_and_eviction_criteria （07: quota / best-effort / persist()）
- **S66** — https://atproto.com/specs/oauth （07: PKCE/DPoP/PAR・token寿命・BFF）
- **S67** — https://atproto.com/guides/about-oauth （07: SPA client・public/confidential・BFF推奨）
- **S68** — https://github.com/bluesky-social/atproto/tree/main/packages/oauth/oauth-client-browser （07: SPA client README）
