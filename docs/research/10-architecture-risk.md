# 10 — Android 推奨アーキテクチャ + 実機で潰すべき未検証点 + extrinsic 制約

入力（唯一の sources of truth）: `06-native-kotlin-twa.md`、`08-comparison-matrix.md`、`09-feature-placement.md`。
補助的に 08 が集約する 02–07 の記載（`S<n>` トークン）と `04-capacitor.md`、`03-pwa-background.md` を参照する。
本ファイルは**新規の一次情報・新規 URL を追加しない**。`S<n>` はすべて `08-comparison-matrix.md` の `## 出典` で URL に解決される。

- アクセス日（02–07 / 08 準拠）: **2026-10-16**
- 本ファイルは todo 11（レポート本体）の入力。**推奨構成は 1 案のみ**、未検証点は「確定事実」ではなく「実機 PoC で確かめる対象」として列挙する。

> 位置づけ: 本ファイルは 2 種類の記述を厳密に分離する。
> **(A) 推奨構成** = 09 の機能配置（＝ 08 の verdict）に接地する、実装方針の選択。
> **(B) 未検証点 / extrinsic 制約** = 02–09 の一次情報では確定できず、実機検証・プラットフォーム審査・関係者合意に委ねる事項。
> (B) は (A) の前提を崩し得るが、本ファイルでは断定しない。

---

## 1. 推奨構成（1 案） — 「Android で CHAIN を作る」

**結論（1 案）: Next.js（static export `out/`）を Capacitor シェルで包み、すれ違い中核（BLE advertise + scan + 継続）はネイティブ Kotlin 実装（Capacitor プラグイン経由）＋ `connectedDevice` 型 Foreground Service で担い、SNS 層（AT Protocol・SNS 共通機能）は Web/WebView 側で実装する。**

```
[Capacitor Android app]
  ├─ WebView 層 : Next.js static export (app UI / SNS / AT Protocol SPA / Web Push)
  │                 └─ attribute: 08 純PWA列・Capacitor列の Web 層セル（Web資産/カメラ・保存/AT Protocol/プッシュ通知）
  └─ Native 層  : Kotlin Capacitor plugin
                    ├─ BluetoothLeAdvertiser  (BLUETOOTH_ADVERTISE)   … すれ違い emit
                    ├─ BluetoothLeScanner     (BLUETOOTH_SCAN)        … すれ違い receive
                    └─ FGS type=connectedDevice                        … BLE 接続そのものを FGS 内で維持
```

### 1.1 層別スタック（どの層がどの技術か）

| 層 | 採用技術 | 根拠（08 該当セル / 09 placement / 06・04） |
| --- | --- | --- |
| 配布・アプリシェル | **Capacitor**（Android native app = WebView + ローカル静的 HTTP サーバ） | 08 Capacitor行「配布=可（S28,S30）」、04 §6（WebView ベース・web assets ディレクトリ配信） |
| Web 資産 / UI | **Next.js static export**（`output:'export'` → `out/` を web assets として配信。SSR 不可） | 08 Capacitor行「Web資産(Next.js)再利用=可（static export必須・SSR不可）（S28,S29,S30）」、09 すれ違い/Party「Capacitor(UI)」、04 §6.2 |
| すれ違い中核 — advertise | **ネイティブ Kotlin `BluetoothLeAdvertiser`**（`BLUETOOTH_ADVERTISE`） | 08 ネイティブKotlin行「advertise=可（S50,S46）」、09 すれ違い行「ネイティブ必須(BLE advertise/scan/BG)」、06 §1 |
| すれ違い中核 — scan | **ネイティブ Kotlin `BluetoothLeScanner`**（`BLUETOOTH_SCAN`、必要なら `neverForLocation`） | 08 ネイティブKotlin行「scan=可（S51,S46）」、09 すれ違い行、06 §1 |
| すれ違い — バックグラウンド維持 | **`connectedDevice` 型 Foreground Service**（BLE 接続そのものを FGS 内で張る） | 08 ネイティブKotlin行「BG BLE=可／FGS=可（S47,S48,S49）」、04 §3.3（FGS を立てるだけでは不足・BLE 接続を FGS 内に移す必要：S26） |
| JS ↔ native 橋渡し | **Capacitor ネイティブプラグイン（Kotlin）** | 09 すれ違い/Party「Capacitor(UI) ＋ ネイティブ必須(BLE)」、04 §3–4（community は central 専用／advertise は別プラグイン：S24,S25） |
| 常時位置共有（フレンド） | **ネイティブ位置**（前面のみの共有なら Web 層可） | 08 純PWA行「バックグラウンド位置=不可（S21,S22）」、08 Capacitor行「バックグラウンド位置=要native（S27,S57）」、09 フレンド行 |
| SNS 層 — AT Protocol | **Web/WebView SPA** `@atproto/oauth-client-browser` ＋ **BFF 推奨** | 08 純PWA行「AT Protocol=可（SPA・BFF推奨）（S66,S67,S68）」、09 AT Protocol行、07 §2 |
| SNS 共通（instance / moderation / Pin / profile / memory / 統計） | **Web 層**（fetch / BFF、IndexedDB / OPFS、KaTeX / marked） | 09 instance/moderation/Pin/profile/memory/統計（純PWAで可）、08 純PWA行「カメラ・保存=可（S56,S58,S61）」「Web資産=可（S29,S57）」、07 §1 |
| notification | **Web Push（FCM 経由・要可視通知）** | 08 全行「プッシュ通知=可（FCM・要可視通知）（S19,S20）」、09 notification行 |
| グループ（前面 GPS + サーバ geofence） | **Web 層**（前面位置 + サーバ判定） | 08 純PWA行「バックグラウンド位置=不可（前面のみ可）（S21,S22）」、09 グループ行 |

### 1.2 なぜこの 1 案か（08/09 に接地した rationale）

- **BLE を Web 層に置けないことが構成を決める。** 08 純PWA行は advertise=不可（S1,S6）・BG BLE=不可（S23,S2）、TWA も同様（S6,S53）で、09 はすれ違い/Party を「ネイティブ必須(BLE advertise/scan/BG)」に配置した。advertise（emit）と passive scan（receive）の両方が要る（01 前提）ため、Web のみでは成立しない。
- **ネイティブ Kotlin が唯一 advertise+scan+BG をフルに満たす。** 08 ネイティブKotlin行は advertise=可（S50）・scan=可（S51）・BG BLE/FGS=可（S47,S48,S49）。06 §1 がこの権限 trio と FGS type を一次情報で示す。
- **それでも Web 層を残すのは、UI と SNS が Web で成立するから。** 09 は UI を Capacitor、SNS 共通機能・AT Protocol を純PWA（＝Web/WebView）で可とした。08 純PWA/Capacitor 行の Web 層セル（Web資産=S29,S57／カメラ・保存=S56,S58／AT Protocol=S66,S67,S68／プッシュ通知=S19,S20）がこれを支える。07 §2 はブラウザ SPA での AT Protocol 駆動を可（BFF 推奨）とする。
- **Capacitor をシェルに選ぶ理由は「Next.js 資産の再利用 + native プラグイン拡張」。** 04 §6 のとおり Capacitor は WebView + ローカル静的サーバで `out/` を配信でき（static export 前提）、ネイティブプラグインで BLE/FGS を足せる。TWA では advertise/BG が不可（S6,S53）で、WebView も `navigator.bluetooth` を公開しない（06 S52）ため、**BLE は Web ではなく必ず native 側**に置く。
- **SNS を Web/WebView に残す理由は移植性。** AT Protocol はブラウザ SPA で駆動でき（07 §2）、純PWA でも Capacitor WebView でも同じ Web 実装が載る。BFF を挟めば public client の短いリフレッシュ寿命（07 §2.4）を緩和できる。

> 注: 本ファイルは**この 1 案のみ**を推奨構成として提示する。代案（純ネイティブ全体、Flutter、Nearby Connections 単独など）は 08 の比較に委ね、ここでは推奨しない。

---

## 2. 実機で潰すべき未検証点（>=5）

以下は**すべて未検証**であり、確定事実ではない。02–09 の一次情報はプラットフォーム仕様・API 可否の層までを扱い、
**OEM 実装差・ハードウェア差・審査運用・実測値・ステークホルダー合意**には踏み込まない。各項目に「何が未検証か」と「なぜ未検証か」を付す。

### 未検証 U1 — Android ベンダー別のバックグラウンド BLE 継続挙動

- **何が未検証か**: `connectedDevice` 型 FGS を立て、その中で BLE advertise/scan を継続したとき、各社端末（Samsung / Xiaomi / Oppo / Huawei / Pixel 等）で FGS が kill されず、advertise が止まらないか。各社の省電力・自動起動制限下で BLE が生きたままかは未確認。
- **なぜ未検証か**: 02–09 が接地するのは AOSP / Android Developers の**プラットフォーム仕様**（06: S47,S48,S49）であり、OEM 固有の省電力実装（バッテリー最適化・バックグラウンド制限）は公開仕様がなく端末依存。**端末ベンダー差**は実機でしか確認できない。04 §3.1（S26）の「数分で中断」報告も一般論で、機種別の閾値は示されない。

### 未検証 U2 — Coded PHY / Extended Advertising の端末差

- **何が未検証か**: すれ違いで使う広告フォーマット（legacy 31 バイトか Bluetooth 5 extended か、Coded PHY による長距離化を採るか）が、対象 Android 端末群で**送信側・受信側とも**成立するか。chipset / Android バージョン差で受信可否・到達距離が変わる可能性。
- **なぜ未検証か**: 04 は `allowExtendedAdvertising` について「Android は既定で legacy のみ報告する」と述べる（S24）が、これはプラグインの**受信側オプション説明**であり、端末個別の Coded PHY / extended 対応可否は 02–09 が網羅しない。**ハードウェア / PHY 対応差**は実機 PoC 領域。

### 未検証 U3 — FGS の UX と Google Play 審査の実運用

- **何が未検証か**: 常駐 FGS の可視通知（ongoing notification）が CHAIN の UX として許容されるか、また `connectedDevice` 型 FGS が Google Play 審査で「正当な用途」として通過するか。
- **なぜ未検証か**: 06（S48,S49）は「type 宣言・対応 permission が必須」「Android 12+ の BG 開始制限」という**技術要件**を示すが、審査ポリシーの適用実例・UX 受容性は 02–09 に一次情報がない。**審査は案件ごとの判断**であり、実提出しないと確定しない（§3 の extrinsic 制約とも関連）。

### 未検証 U4 — 電池消費

- **何が未検証か**: advertise 常時 + passive scan + FGS 常駐を同時に走らせた場合の 1 日あたりバッテリー持ち・発熱。
- **なぜ未検証か**: 02–09 は API の**可否**（03 / 06）を扱うが、消費電力の実測値は持たない。SoC・チップ・電波環境・ユーザの使用パターンに強く依存し、**実機計測が必要**。

### 未検証 U5 — Nearby Connections を採る場合の実挙動

- **何が未検証か**: Nearby Connections（Google Play Services）を BLE すれ違いの代替・併用に使う場合の、接続戦略（Cluster / Star / Point-to-Point）ごとの発見レイテンシ・成功率・バックグラウンド維持・電池・両端末の GMS 状態依存。
- **なぜ未検証か**: 05（S41,S43,S44）は「Android-only・GMS 必須・戦略がある」ことまでを示すが、実機での発見成功率や BG 継続は記載がない。加えて **iOS 非対応**（S41,S43,S44）のためクロスプラットフォーム前提が崩れる。**採用判断の前に実機 PoC が要る**。

### 未検証 U6 — 「iOS は非対応」という前提の関係者合意

- **何が未検証か**: CHAIN が iOS を明示的に対象外とする前提が、要件・体制として合意されているか。本推薦（Android native Kotlin / Capacitor）は Android 専用構成であり、iOS 展開は別実装になる。
- **なぜ未検証か**: これは技術ではなく**要件 / 体制の未確定事項**。02–09 は iOS を対象にしておらず、「iOS 非対応」は**本推薦のスコープ宣言**であって確定した製品判断ではない。（参考として 05 は Flutter の iOS `bluetooth-peripheral` BG mode に触れるが：S37,S38、これは別スタックの話で本推薦の iOS 対応を意味しない。）よって関係者合意は未検証。

### 未検証 U7 — WebView 内の Web Bluetooth シム経由のすれ違い挙動

- **何が未検証か**: Capacitor の WebView は Android WebView であり `navigator.bluetooth` を公開しない（06: S52）。`shimWebBluetooth` 等のシム（04: S25）経由で、すれ違いの実運用（継続 scan / advertise）が native Kotlin 直実装と同等に機能するか。
- **なぜ未検証か**: 04（S24,S25）はシムの**存在**を示すが、シム経由の継続 scan / BG での挙動は一次情報にない。ネイティブプラグイン直呼びとの差分は**実機検証が必要**。本推薦はシムに依存せず native 直実装を採るが、シム併用の可否は未確定のまま残す。

### 未検証点の集計

| # | 未検証点 | 類型 | 主な接地 |
| --- | --- | --- | --- |
| U1 | ベンダー別 BG BLE 継続 | 端末ベンダー差 | 06（S47,S48,S49）/ 04（S26） |
| U2 | Coded PHY / Extended Advertising | ハードウェア差 | 04（S24） |
| U3 | FGS の UX / Play 審査 | 審査・UX | 06（S48,S49） |
| U4 | 電池消費 | 実測 | 03 / 06（可否のみ） |
| U5 | Nearby Connections 実挙動 | API 実挙動 | 05（S41,S43,S44） |
| U6 | iOS 非対応の前提共有 | 要件 / 体制 | スコープ宣言 |
| U7 | WebView シム経由の BLE | 実装差 | 06（S52）/ 04（S24,S25） |

**未検証点は 7 件（>=5）。すべて「実機 PoC / 審査 / 関係者合意で潰す」対象であり、確定事実として扱わない。**

---

## 3. extrinsic 制約（プラットフォーム制約であり、法的助言ではない）

本節は **Android / Google Play というプラットフォーム側の要件・審査観点**の整理である。
**適法性・法令適合の判断（法的助言）ではない**。適法性の最終判断は専門家・法務に確認すること。

### 3.1 Google Play「付近のデバイス」権限審査（BLUETOOTH_SCAN / ADVERTISE / CONNECT）

- **プラットフォーム側の権限モデル（確定）**: Android 12（target API 31）以降、`BLUETOOTH_SCAN` / `BLUETOOTH_ADVERTISE` / `BLUETOOTH_CONNECT` は**runtime permission** であり、利用前にユーザー承認を明示的に要求する必要がある（06: S46。逐語:「The BLUETOOTH_ADVERTISE, BLUETOOTH_CONNECT, and BLUETOOTH_SCAN permissions are runtime permissions. Therefore, you must explicitly request user approval…」）。
- **`neverForLocation` のトレードオフ（確定）**: `BLUETOOTH_SCAN` に `android:usesPermissionFlags="neverForLocation"` を宣言すると、位置権限（`ACCESS_FINE_LOCATION`）を外せる代わりに、「interact できる Bluetooth デバイスの種別が制限され得る」（06: S46。逐語:「may restrict the types of Bluetooth devices you can interact with」）。すれ違いのデバイス発見範囲に影響し得る。
- **ストア審査の観点（extrinsic・要確認）**: Google Play は「付近のデバイス（Nearby devices）」系の権限を**審査対象**とし、用途の正当性・プライバシーポリシー・データ利用の説明を求める運用がある。CHAIN のように**すれ違い（advertise + scan）で近接端末を継続的に探索**するアプリは、この審査区分に該当し得る。
  - これは**プラットフォームの審査要件**であり、**具体的な審査基準・通過可否は提出時点の Play ポリシーに依存する**（本ファイルは特定のポリシー文言を確定事実として断定しない）。→ **未検証点 U3 と連動**。
- **設計上の含意**: 権限要求のタイミング・理由表示（rationale）と、`neverForLocation` を採るか位置権限を採るかの選択が、審査と UX の両方に効く。これは 06 の権限 trio・neverForLocation 記述（S46）に接地する。

### 3.2 プライバシー（位置情報 / すれ違い履歴の保存）

- **位置情報**:
  - プラットフォーム仕様上、Web の Geolocation は `[Exposed=Window]` で**前面ページが無いと使えない**（03: S21,S22）。フレンドの「常時位置共有」を機能させるにはネイティブ位置が必要（09 フレンド行・§1.1）。**位置を常時扱う設計は、権限・データ保護の論点を必然的に伴う**（プラットフォーム制約）。
  - すれ違いの受信は `neverForLocation` により「位置を導出しない」強い表明を置ける（06: S46）が、CHAIN は位置共有機能（フレンド / グループ）を別途持つため、**位置とすれ違いのデータを分離して扱えるか**が設計・審査の論点になる。
- **すれ違い履歴の保存**:
  - token はサーバ発行（01 前提2）。すれ違い履歴の**保存先（サーバ / ローカル）・保持期間・他端末への伝播**はプライバシー設計の論点であり、02–09 の一次情報では確定しない。
  - ローカル保持は origin 単位のクォータ制約下にあり、既定は best-effort（容量逼迫・ユーザー操作で削除され得る）で、恒久化には `persist()` が要る（07: S61。08 純PWA行「カメラ・保存=S56,S58,S61」）。→ 履歴の長期保持をローカルに依存する設計は、消去前提となる。
  - 通知は Web Push 経由で「要可視通知」が必須（08 全行・S19,S20）。すれ違い数の通知内容にもプライバシー配慮が要る（プラットフォーム制約）。
- **フレーミング**: 以上はすべて **プラットフォーム仕様・審査要件・ストレージ制約**の観点であり、**法的助言ではない**。個人データの適法な取り扱い（同意・保存期間・越境移転等）の判断は、法務・専門家の確認事項とする。

### 3.3 extrinsic 制約の集計

| # | extrinsic 制約 | 種別 | 主な接地 |
| --- | --- | --- | --- |
| E1 | 「付近のデバイス」権限審査（SCAN/ADVERTISE/CONNECT） | プラットフォーム審査 | 06（S46） |
| E2 | `neverForLocation` のトレードオフ（発見範囲制限） | プラットフォーム仕様 | 06（S46） |
| E3 | 位置情報の取り扱い（前面限定・ネイティブ要） | プラットフォーム仕様 / プライバシー | 03（S21,S22） |
| E4 | すれ違い履歴の保存（保存先・保持・best-effort） | プライバシー / ストレージ | 07（S61）/ 01 前提2 |

---

## 4. 06 / 09 との整合

| 本ファイルの記述 | 06 / 08 / 09 の対応 | 整合 |
| --- | --- | --- |
| BLE は Web では不可 → native Kotlin に置く | 06 §2（WebView は `navigator.bluetooth` 非公開：S52）／08 純PWA・TWA の advertise/BG=不可（S1,S6,S23,S53） | 一致 |
| advertise/scan/BG は native Kotlin が担う | 06 §1（`BluetoothLeAdvertiser`/`BluetoothLeScanner`/`connectedDevice` FGS：S50,S51,S48）／08 ネイティブKotlin行 | 一致 |
| FGS を立てるだけでなく BLE 接続を FGS 内で張る | 04 §3.3（robingenz 報告：S26） | 一致 |
| UI は Capacitor（Next.js static export） | 04 §6（WebView + static export：S28,S29,S30）／09 すれ違い/Party「Capacitor(UI)」 | 一致 |
| SNS / AT Protocol は Web 層（BFF 推奨） | 08 純PWA行 AT Protocol=可（S66,S67,S68）／09 AT Protocol行／07 §2 | 一致 |
| 常時位置共有はネイティブ、前面のみなら Web | 08 純PWA行 BG位置=不可（S21,S22）／09 フレンド行 | 一致 |
| iOS 非対応はスコープ宣言（未合意） | 本推薦は Android 専用（06/08 の Android 前提）／09 は Android 配置 | 未検証 U6 として明示 |

矛盾: 検出なし。ただし U1–U7・E1–E4 は**推奨構成の前提を揺らし得る未解決事項**であり、確定事実ではない。

---

## 出典

本ファイルは新規出典を追加しない。`S<n>` トークンの URL はすべて `08-comparison-matrix.md` の `## 出典`（= 02–07 引用 URL）で解決される。
参照した一次ファイル: `03-pwa-background.md`、`04-capacitor.md`、`06-native-kotlin-twa.md`、`07-sns-atproto.md`、`08-comparison-matrix.md`、`09-feature-placement.md`。
