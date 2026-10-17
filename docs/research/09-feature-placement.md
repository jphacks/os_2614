# 09 — CHAIN 機能別 推奨配置表（機能 → 推奨技術）

入力（唯一の sources of truth）: `01-requirements.md`（CHAIN の 11 機能行 + `AT Protocol` 1 行 = 12 行）と
`08-comparison-matrix.md`（7 アーキテクチャ × 13 軸のマトリクス。各セルの `S<n>` トークンは 08 の `## 出典` で URL に解決）。

- 本ファイルは新規の一次情報・新規 URL を追加しない。根拠は 01 / 08（および 08 が集約する 02–07）に接地する。
- 本表は todo 10（Android アーキテクチャ推薦）の入力。機能名は `01-requirements.md` の要求マトリクスと完全一致させる。

## 推奨技術の記号（placement bucket）

| bucket | 意味 |
| --- | --- |
| 純PWAで可 | ブラウザ（`navigator.*` のみ、ネイティブラッパーなし）で実現できる |
| Capacitorで可 | Capacitor（WebView ラッパー + プラグイン）で実現できる |
| ネイティブ必須 | JS/Web 層では不可で、ネイティブ実装（Service/FGS・BLE API 等）が必須 |
| 不可 | 02–07 の一次情報で実現不可 |

組合せ表記（複数層にまたがる機能）も用いる。例: `純PWA(UI) + ネイティブ必須(BLE)`。

## 機能別 推奨配置表

| 機能 | 推奨技術 | 配置の根拠（08 の該当セル） | 備考 |
| --- | --- | --- | --- |
| すれ違い機能 | ネイティブ必須(BLE advertise/scan/BG) ＋ Capacitor(UI) | 08 純PWA行「BLE advertise可否=不可（S1,S6）」・「BLE passive scan可否=条件付き(flag)(S1,S3,S5,S7)」・「バックグラウンドBLE=不可（S23,S2）」、08 Capacitor行「advertise=別プラグインで可（S24,S25）」・「BG=要native+FGS（S26）」、08 ネイティブKotlin行「advertise/scan/BG=可（S50/S51/S47/S48）」 | 双方向認識は advertise（emit）と passive scan（receive）の両方が必須（01 行19）。token はサーバ発行（01 前提2）。BG BLE が純PWA不可のため native 必須 |
| フレンド機能 | 純PWA(UI) + ネイティブ必須(常時位置共有) ＋ サーバgeofence | 08 純PWA行「バックグラウンド位置=不可（Exposed=Window）（S21,S22）」、08 Capacitor行「バックグラウンド位置=要native（S27,S57）」、08 ネイティブKotlin行「バックグラウンド位置=条件付き（S47,S49）」 | 「位置情報の常時共有」は BG 位置＝純PWA不可（01 行20）。前面のみの共有に限れば純PWA(UI)で可 |
| Party | Capacitor(UI) + ネイティブ必須(BLE 在席判定) | 08 純PWA行「BLE advertise可否=不可（S1,S6）」・「バックグラウンドBLE=不可（S23,S2）」、08 Capacitor行「advertise=別プラグイン（S24,S25）」・「BG=要native+FGS（S26）」、08 全行「カメラ・保存=可（S56,S58）」 | 30秒仮→5分本の在席判定に継続 BLE が要る（01 前提8）。写真は PWA カメラで可（01 行21）。party 内通信の relay 可否は未確定（01 未確定） |
| グループ | 純PWAで可（前面位置 + サーバgeofence） | 08 純PWA行「バックグラウンド位置=不可（S21,S22）＝前面のみ可」・「プッシュ通知=可（S19,S20）」・「AT Protocol=可（S66,S67,S68）」、08 ネイティブKotlin行「バックグラウンド位置=条件付き（S47,S49）」 | GPS は教室等の前面利用（01 前提7）。「エリアに入った時に通知」を BG 検知で行うならネイティブ必須、前面/サーバ geofence なら純PWAで可 |
| instance=(グループ/party内) | 純PWAで可（Web UI + サーバrelay） | 08 純PWA行「Web資産(Next.js)再利用=可（S29,S57）」・「カメラ・保存=可（S56,S58）」・「AT Protocol=可（S66,S67,S68）」、07 §1「instance コンテンツ（テキスト/画像/markdown/tex）=実現可」 | in-app camera の要否は未確定（01 未確定）。内部通信が BLE かサーバ relay かも未確定（01 未確定） |
| moderation | 純PWAで可（fetch/BFF 経由で外部API） | 08 純PWA行「AT Protocol=可（SPA・BFF推奨）（S66,S67,S68）」・「Web資産(Next.js)再利用=可（S29,S57）」、07 §1.2「Jev 外部API呼び出し=実現可（fetch/BFF）」 | Jev によるサーバ側テキスト検閲（01 行24）。CORS 可否は未確認（07 §1.2） |
| Pin | 純PWAで可（サーバ側判定 + UI） | 08 純PWA行「AT Protocol=可（SPA・BFF推奨）（S66,S67,S68）」・「Web資産(Next.js)再利用=可（S29,S57）」、07 §1「moderation/Pin=実現可」 | 「大事な情報は自動でPin」はサーバ側判定（01 行25）。UI は Web 層で可 |
| notification | 純PWAで可（Web Push / FCM） | 08 全行「プッシュ通知=可（FCM・要可視通知）（S19,S20）」、07 §1「notification（Web Push）=実現可」 | フレンド申請・すれ違い数通知（01 行26）。集計元は BLE（すれ違い機能行）とサーバ。iOS は Home Screen web app 限定（03） |
| profile | 純PWAで可（Web UI + IndexedDB/サーバ） | 08 純PWA行「カメラ・保存=可（IndexedDB・OPFS・quota）（S56,S58,S61）」・「Web資産(Next.js)再利用=可（S29,S57）」、07 §1「profile=実現可」 | id/名前/アイコン等はサーバ保持（01 行27）。AT Protocol 上の profile かは未確定（01 行27） |
| memory | 純PWAで可（IndexedDB/OPFS + サーバ） | 08 純PWA行「カメラ・保存=可（IndexedDB・OPFS・quota）（S56,S58,S61）」、07 §1.1「quota / best-effort / persist()」、07 §1「memory=実現可」 | archive party の保存先はサーバ/ローカル未確定（01 行28）。quota 超過 eviction 前提で原典はサーバ保持が安全（07 §1.1） |
| 統計(その他) | 純PWAで可（Web UI + サーバ保存） | 08 純PWA行「カメラ・保存=可（IndexedDB）（S56,S58,S61）」・「Web資産(Next.js)再利用=可（S29,S57）」、07 §1「profile/memory/統計=実現可」 | 「統計データは backend に保存したい」（01 前提10）。クライアント完結案も可（01 行29） |
| AT Protocol(SNS Protocol) | 純PWAで可（ブラウザSPA・BFF推奨） | 08 純PWA行「AT Protocol可否=可（SPA・BFF推奨）（S66,S67,S68）」、07 §2「ブラウザSPA実装可（BFF推奨）」 | public client はリフレッシュ寿命が短い（目安2週間）ため BFF 推奨（07 §2.4）。Space/instance 対応は論点（01 行30） |

行数: **12 行**（`01-requirements.md` 要求マトリクスの 11 機能行 + `AT Protocol(SNS Protocol)` 1 行 = 12 行と一致。未割当 0）。

## まとめ（03–07 との対応）

- **BLE 系（すれ違い・Party）** は 08 純PWA行の advertise=不可／BG BLE=不可 → ネイティブ必須（Capacitor は UI 層のみ可）。02/03/04/06 の verdict と一致。
- **常時位置共有（フレンド）** は 08 純PWA行バックグラウンド位置=不可 → 位置成分はネイティブ必須。前面のみなら純PWA可。
- **GPS 前面 + サーバ geofence（グループ）**・**UI/外部API/保存系（instance, moderation, Pin, notification, profile, memory, 統計）**・**AT Protocol** は 08 純PWA/Capacitor 行の Web 層セル（カメラ・保存/Web資産/プッシュ通知/AT Protocol）で可。

## 出典

本ファイルは新規出典を追加しない。`S<n>` トークンの URL はすべて `08-comparison-matrix.md` の `## 出典`（=02–07 引用 URL）で解決される。
