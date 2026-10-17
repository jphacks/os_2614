# 04 — Capacitor 実現性: BLE / 広告(advertise) / バックグラウンド / Foreground Service

- 調査日 (accessed): **2026-10-16**
- 方針: 一次情報のみ。各主張に **source URL + 逐語引用 + アクセス日** を付す。
- 結論 (verdict):
  - **central (BLE セントラル) = 可** (`@capacitor-community/bluetooth-le`)
  - **advertise (ペリフェラル/広告) = 別プラグインで可** (`@capgo/capacitor-bluetooth-low-energy` の `startAdvertising`/`stopAdvertising`。community 版は central 限定)
  - **BG (Android バックグラウンド) = 要 native Service・FGS** (JS だけでは不可。BLE 接続そのものを Foreground Service 内へ移す必要)

> [!WARNING]
> **Capacitor を入れただけでは advertising は有効にならない。** community 版 BLE プラグインは central 専用であり、ペリフェラル/広告とバックグラウンド維持は別プラグインまたはネイティブ実装が必須。

---

## 1. 判定テーブル (capability × verdict × source × quote × date)

| capability | Capacitor verdict | source URL | exact quote | date |
|---|---|---|---|---|
| BLE central (scan/connect/read/write/notify) | 可 (central 専用) | https://github.com/capacitor-community/bluetooth-le | "Furthermore the plugin only supports the central role of the Bluetooth Low Energy protocol. If you need the peripheral role, take a look a these plugins" | 2026-10-16 |
| BLE 継続スキャン `requestLEScan` | 可 (central 側) | https://github.com/capacitor-community/bluetooth-le | "Start scanning for BLE devices to interact with according to the filters in the options. The callback will be invoked on each device that is found. Scanning will continue until `stopLEScan` is called." | 2026-10-16 |
| Android12 位置情報不要フラグ `androidNeverForLocation` | 可 (オプション) | https://github.com/capacitor-community/bluetooth-le | "If your app doesn't use Bluetooth scan results to derive physical location information, you can strongly assert that your app doesn't derive physical location. (Android only)" | 2026-10-16 |
| Bluetooth5 拡張広告の受信 `allowExtendedAdvertising` | 可 (受信側オプション, Android) | https://github.com/capacitor-community/bluetooth-le | "Report Bluetooth 5 advertising extensions in addition to legacy advertisements (Android only). Android only reports legacy advertisements by default, so a device that advertises more than the 31 bytes of a legacy advertisement is not discovered at all." | 2026-10-16 |
| BLE peripheral / advertise (community プラグイン) | **不可** | https://github.com/capacitor-community/bluetooth-le | "the plugin only supports the central role of the Bluetooth Low Energy protocol" | 2026-10-16 |
| BLE peripheral / advertise (別プラグイン) | **別プラグインで可** | https://capgo.app/docs/plugins/bluetooth-low-energy/ | "startAdvertising Start advertising as a peripheral (BLE server)." | 2026-10-16 |
| advertise 停止 | 可 (capgo) | https://capgo.app/docs/plugins/bluetooth-low-energy/ | "stopAdvertising Stop advertising." | 2026-10-16 |
| Foreground Service ヘルパ (BLE 接続維持) | 可 (capgo, Android only) | https://capgo.app/docs/plugins/bluetooth-low-energy/ | "startForegroundService Start a foreground service to maintain BLE connections in background (Android only)." | 2026-10-16 |
| Foreground Service 停止 | 可 (capgo, Android only) | https://capgo.app/docs/plugins/bluetooth-low-energy/ | "stopForegroundService Stop the foreground service (Android only)." | 2026-10-16 |
| Android バックグラウンド BLE 継続 | **要 native Service・FGS** | https://github.com/capacitor-community/bluetooth-le/issues/643 | "On Android the communication is interrupted after a few minutes Minutes when the app (Ionic/React) is in background. This comes from the Android OS power saving." | 2026-10-16 |
| バックグラウンドでの JS 実行 (WebView) | **不可** (Activity が再開されない) | https://github.com/capacitor-community/bluetooth-le/issues/643 | "But with a hybrid capacitor app, the Activity is not restarted and therefore your javascript won't be executed." | 2026-10-16 |
| 恒常バックグラウンド処理 (OS 制約) | **不可** (常時稼働の BG サービスは存在し得ない) | https://github.com/ionic-team/capacitor-background-runner/ | "It's not possible to run persistent, always running background services on mobile operating systems." | 2026-10-16 |
| Next.js / web assets の再利用 | 可 (WebView に静的アセットを配信) — ただし SSR 不可・static export 必須 | https://ionic.io/blog/how-capacitor-works-2 | "your web app runs inside of a Web View which is a native OS control that provides a streamlined, chrome-less browser instance." | 2026-10-16 |
| Next.js SSR (server 必要機能) | **不可** (Capacitor シェルに Node サーバは無い) | https://nextjs.org/docs/app/guides/static-exports | "Features that require a Node.js server, or dynamic logic that cannot be computed during the build process, are not supported" | 2026-10-16 |

**Verdict line:** central=可 / advertise=別プラグインで可 / BG=要native Service・FGS

---

## 2. `@capacitor-community/bluetooth-le` — central 専用である根拠

リポジトリ: https://github.com/capacitor-community/bluetooth-le (accessed 2026-10-16)

README (main ブランチ) の Introduction より逐語:

> "Furthermore the plugin only supports the central role of the Bluetooth Low Energy protocol. If you need the peripheral role, take a look a these plugins:
> - https://github.com/randdusing/cordova-plugin-bluetoothle
> - https://github.com/don/cordova-plugin-ble-peripheral"

- **central ロールのみ**。peripheral(= 広告を出す側)は非対応で、Cordova プラグインを紹介するに留まる。
- `requestLEScan` 実装あり (継続スキャン、`stopLEScan` まで継続) — 逐語:
  > "requestLEScan(options: RequestBleDeviceOptions, callback: (result: ScanResult) => void) => Promise<void>" / "Scanning will continue until `stopLEScan` is called."
- オプション `androidNeverForLocation` (default `false`) — 逐語:
  > "If your app doesn't use Bluetooth scan results to derive physical location information, you can strongly assert that your app doesn't derive physical location. (Android only) Requires adding 'neverForLocation' to AndroidManifest.xml"
- オプション `allowExtendedAdvertising` (default `false`, Android only) — 逐語:
  > "Report Bluetooth 5 advertising extensions in addition to legacy advertisements (Android only). Android only reports legacy advertisements by default, so a device that advertises more than the 31 bytes of a legacy advertisement is not discovered at all. Enabling this reports both legacy and extended advertisements, so nothing is lost."

補足: 上記 2 オプションはいずれも **スキャン(受信)側**の制御であり、ペリフェラルとして広告を **発信する** API ではない。advertise は本プラグインには存在しない。

---

## 3. Background BLE の弱点 — issue #643

Issue: https://github.com/capacitor-community/bluetooth-le/issues/643 (accessed 2026-10-16)
タイトル: **"Foreground Service on Android"** / state: **open** / 作成 2024-04-09。

### 3.1 問題の起票 (2024-04-09) 逐語

> "On Android the communication is interrupted after a few minutes Minutes when the app (Ionic/React) is in background. This comes from the Android OS power saving. ... On Android the BLE service needs to be moved to a foreground service, otherwise it gets interrupted."

### 3.2 維持者 peitschie の説明 (2024-10-30) — WebView Activity が再開されない

逐語:

> "A capacitor app is a Process that runs an Activity which contains the web browser used to execute your application's javascript code. That is, in order for your javascript to be executed, android needs to run a Process -> launch an Activity -> render your code in the web browser."

> "The key difference between using the callback-based scan and the pending intent based scan is that the pending intent scan can relaunch your applications process if Android has terminated the process for power reasons or due to the user navigating too far away. But with a hybrid capacitor app, the Activity is not restarted and therefore your javascript won't be executed."

つまり **pending intent による再起動でも、Hybrid アプリでは Activity が再開されないため JS が動かない**。

> "For less controlled environments, you likely need to write native code (e.g., some kind of service) that can execute without the capacitor ecosystem going."

→ **受け入れられた修正方針 = BLE を native Service 側へ移す**。

同コメントは 2 つの「停止に見える」現象も挙げる:

> "5 minutes after the phone screen is turned off, an power optimization in the webview kicks in, and pauses all javascript processing"
> "Roughly 40-50 minutes after the phone screen is turned off (assuming the phone is unplugged), the phone usually enters a deep doze mode." (deep doze でさらに JS 停止)

### 3.3 「FGS 内に実装して動いた」報告 — robingenz (2025-01-19) 逐語

> "The solution was ultimately an Android Foreground Service. This is also one of the two recommendations from the Android documentation"

> "The reason why it didn't work for @rainerfritz with the `@capawesome-team/capacitor-android-foreground-service` is that you don't just need an active Foreground Service, but that the Bluetooth connection must be established via this Foreground Service (at least that was our key finding). **So we created a Capacitor plugin with the Bluetooth implementation in the Foreground Service and it now works perfectly.**"

→ **要点: FGS を立てるだけでは不足。BLE 接続そのものを FGS(= native Service)内で張ることが必須**。ここまでやって初めて「works perfectly」。

（参考: 同スレッドで omercnet が Pending Intent + receiver 方式を PR #700 として提案・議論されたが、peitschie は「pending intent は既存 callback 方式と比べて優れない」と述べ、根本解決は native Service と整理している。）

---

## 4. 代替プラグイン: `@capgo/capacitor-bluetooth-low-energy`

- パッケージ名 (検証済み): **`@capgo/capacitor-bluetooth-low-energy`**
  - ドキュメント: https://capgo.app/docs/plugins/bluetooth-low-energy/ (accessed 2026-10-16)
  - npm registry: https://registry.npmjs.org/@capgo/capacitor-bluetooth-low-energy — `name: @capgo/capacitor-bluetooth-low-energy`, dist-tags `latest: 8.2.7`, `modified: 2026-10-15` (accessed 2026-10-16)
- ドキュメント「Public API」表より逐語 (advertise / FGS 関連):
  > "startAdvertising Start advertising as a peripheral (BLE server)."
  > "stopAdvertising Stop advertising."
  > "startForegroundService Start a foreground service to maintain BLE connections in background (Android only)."
  > "stopForegroundService Stop the foreground service (Android only)."
- 参考 (同 API 表の関連メソッド): `startScan` / `stopScan`, `connect` / `disconnect`, `startCharacteristicNotifications` / `stopCharacteristicNotifications`, `requestPermissions`, `checkPermissions`, `shimWebBluetooth`（Web Bluetooth シム設置）。

> [!IMPORTANT]
> `@capgo/capacitor-bluetooth-low-energy` は central と **peripheral(advertise)の両方**、さらに **Android Foreground Service 制御** を 1 プラグインで提供する。community 版で不足する advertise と FGS をここで補える。パッケージ名は上記のとおり検証済み。

---

## 5. `capacitor-background-runner` の現状

リポジトリ: https://github.com/ionic-team/capacitor-background-runner (accessed 2026-10-16)

### 5.1 ステータス (検証結果, 2026-10-16)

| 項目 | 値 | 出典 |
|---|---|---|
| archived | `false` (アーカイブされていない) | GitHub API `repos/ionic-team/capacitor-background-runner` |
| pushed_at | `2026-10-16T14:16:29Z` (当日も push あり) | 同上 |
| stars / open issues | 56 / 45 | 同上 |
| GitHub latest release | `1.1.0` (2024-04-23) | GitHub API `releases/latest` |
| npm dist-tags | `latest: 3.0.0`, `latest-7: 2.3.1`, `dev: 3.1.0-dev.20261007...`, `modified: 2026-10-07` | https://registry.npmjs.org/@capacitor/background-runner |

→ **リポジトリは生存・更新継続中、npm も `3.0.0` まで公開継続**。ただし GitHub 上の Release タグは `1.1.0` のまま (npm の方が先行)。

### 5.2 重要: BLE は提供しない (WebView 外の別 JS 環境)

README 逐語:

> "Background Runner provides an event-based standalone JavaScript environment for executing your Javascript code outside of the web view."

> "The challenge with standard Capacitor applications is that the webview is not available when these background events occur, requiring you to write native code to handle these events. This is where the Background Runner plugin comes in."

> "Background Runner does not execute your Javascript code in a browser or web view, therefore the typical Web APIs you may be used to may not be available. This includes DOM APIs nor ability to interact with your application's DOM."

提供される API は **console / TextDecoder / TextEncoder / addEventListener / setTimeout / setInterval / clearTimeout / clearInterval + Geolocation + Notifications + Watch** 等に限定され、**BLE API は無い**。

issue #643 の peitschie (2024-10-30) も逐語:

> "There is an interesting effort underway at https://github.com/ionic-team/capacitor-background-runner/, but unfortunately this does not yet have a suitable mechanism for adding Bluetooth support"

> [!WARNING]
> background-runner は **「WebView の外で JS を動かす」** ための環境であり、**BLE/advertise の代替にはならない**。BLE をバックグラウンドで維持する要件は background-runner では解決できない（BLE 実装は別途 native/FGS 側が必要）。

---

## 6. Next.js / web assets の再利用（asset-reuse note）

### 6.1 Capacitor は WebView ベース

- Ionic "How Capacitor Works" (https://ionic.io/blog/how-capacitor-works-2, accessed 2026-10-16) 逐語:
  > "Capacitor is a cross-platform native runtime for Web Native apps."
  > "Essentially, Capacitor takes modern web apps, and then packages them up to run on iOS, Android, and PWA with access to native platform features and OS-level controls."
  > "This means your web app runs inside of a Web View which is a native OS control that provides a streamlined, chrome-less browser instance."
- Ionic Framework WebView 概念 (https://ionicframework.com/docs/core-concepts/webview, accessed 2026-10-16) 逐語:
  > "The Web View is automatically provided for apps integrated with Capacitor."
  > "For Capacitor apps ... Implementations: iOS : WKWebView / Android : WebView for Android"
  > "Capacitor and Cordova apps are hosted on a local HTTP server and are served with the `http://` protocol."
- Capacitor Web docs (https://capacitorjs.com/docs/web, accessed 2026-10-16) 逐語:
  > "When you're ready to publish your Progressive Web App and share it with the world, just upload the contents of your web assets directory."

→ Capacitor は **ローカル HTTP サーバ + 各 OS の WebView** に **web assets ディレクトリ**を配信するシェル。よってフレームワーク非依存で、**Next.js のビルド成果物 (静的アセット) を web assets として再利用可能**。

### 6.2 ただし SSR は不可 — static export が必須

- Next.js 公式 static export ガイド (https://nextjs.org/docs/app/guides/static-exports, accessed 2026-10-16) 逐語:
  > "To enable a static export, change the output mode inside `next.config.js`: `output: 'export'`"
  > "After running `next build`, Next.js will create an `out` folder with the HTML/CSS/JS assets for your application."
  > "Features that require a Node.js server, or dynamic logic that cannot be computed during the build process, are **not** supported" — 該当リスト: Dynamic Routes, Route Handlers that rely on Request, Cookies, Rewrites, Redirects, Headers, Proxy, ISR, Image Optimization with the default loader, Draft Mode, Server Actions, Intercepting Routes 等。

> [!NOTE]
> **Capacitor シェルには Node.js サーバが存在しない** (WebView + ローカル静的 HTTP サーバのみ)。したがって Next.js をそのまま (SSR / `next start`) 動かすことはできず、**`output: 'export'` による static export の `out/` を web assets として読ませる必要がある**。
>
> - 再利用可: Next.js の **静的エクスポート成果物** (HTML/CSS/JS, SPA 的 client routing, `'use client'` コンポーネント)。
> - 再利用不可: サーバ依存機能 (Request を使う Route Handlers, Cookies, Rewrites/Redirects/Headers, Server Actions, ISR, Draft Mode, 既定ローダの Image Optimization 等)。
> - サーバ側データ取得はビルド時取得 (`fetch` in Server Components / SSG) または client 側取得 (SWR/`'use client'`) に置き換える。

---

## 7. まとめ

- **BLE central**: `@capacitor-community/bluetooth-le` で 可（ただし central 専用）。
- **advertise (peripheral)**: community 版は不可 → `@capgo/capacitor-bluetooth-low-energy` の `startAdvertising`/`stopAdvertising` で 可。
- **Background (Android)**: JS だけでは 不可。**要 native Service・FGS**。`@capgo` の `startForegroundService`/`stopForegroundService` が入口になるが、issue #643 の robingenz 報告どおり **BLE 接続自体を FGS 内で張る** 実装が必須。
- **`@capacitor/background-runner`**: 生存・npm `3.0.0` まで更新継続だが **BLE 非対応**（WebView 外の別 JS 環境）。BLE BG 要件は解決しない。
- **Next.js assets**: WebView に静的アセット配信なので **static export なら再利用可**、**SSR は不可**。
