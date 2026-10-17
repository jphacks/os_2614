# Android native プラグイン仕様

Capacitor アプリの native 層（Kotlin）と、backend・WebView との取り決め。
構成の根拠は `docs/research/report.md` §6（BLE は Web 層に置けないため native で持つ）。
backend 側の対応する仕様は `specification/backend/` の 03・05・06・08。

## 1. 役割分担

| 機能 | 担当 | 理由 |
| --- | --- | --- |
| UI、SNS、instance、WebSocket | WebView（Next.js） | Web 層で成立する |
| ログインの完了とトークン保持 | **native** | バックグラウンドで WebView がない時も native が API を呼ぶため。refresh token の持ち主を 1 つにする（§3） |
| beacon token の取得・保持 | native | |
| BLE advertise / scan | native | Web Bluetooth は advertise・バックグラウンド不可 |
| sighting の集約と送信 | native | FGS の中で動く |
| フレンドの近接検出と Party 報告 | native（目標）。**いまは JS が前面の間だけ行う**（`web/src/device/proximity/party.ts`、`docs/spec/party-implementation.md` §5） | 15 秒間隔の報告をバックグラウンドで続けるため。native への移動は P1 |
| 位置の取得と送信 | native | `isMock` は native でしか取れない（backend 05 §4） |
| 端末のカレンダーの読み取りと送信 | native | 自動グループの予定（backend 09 §8.2）。WebView が閉じていても、FGS の中で同期を続けるため（§11） |
| 予定の手入力 | WebView | 画面から `PUT /me/schedule-today`（`source: "manual"`）を直接呼ぶ |
| Foreground Service | native | |
| プッシュ通知の受信 | Capacitor Push Notifications プラグイン | 既製品で足りる |

## 2. プラグイン API（`ChainProximity`）

WebView から呼ぶ TypeScript の定義。

```ts
export interface ChainProximityPlugin {
  // ---- セッション（§3） ----
  completeLogin(opts: { code: string }): Promise<{ did: string }>;
  getAccessToken(): Promise<{ accessToken: string; expiresAt: string }>;
  logout(): Promise<void>;

  // ---- 権限（§4） ----
  checkPermissions(): Promise<PermissionStatus>;
  requestPermissions(opts: { permissions: Array<'bluetooth' | 'location' | 'notifications' | 'calendar'> }): Promise<PermissionStatus>;

  // ---- すれ違いモード（§5〜§8） ----
  start(): Promise<void>;            // FGS を起動し advertise と scan を始める。前面からのみ呼べる
  stop(): Promise<void>;
  getStatus(): Promise<ProximityStatus>;
  flushSightings(): Promise<{ accepted: number; dropped: number }>;
  refreshFriendBeacons(): Promise<void>;  // WebSocket の friend.changed を受けたら呼ぶ

  // ---- 位置（§9） ----
  setForegroundLocation(opts: { enabled: boolean }): Promise<void>;
  updateLocationNow(): Promise<{ sent: boolean; reason?: 'denied' | 'mock' | 'inaccurate' | 'unavailable' }>;

  // ---- カレンダー（§11） ----
  listCalendars(): Promise<{ calendars: Array<{ id: string; name: string; account: string; color: string; selected: boolean }> }>;
  setSelectedCalendars(opts: { ids: string[] }): Promise<void>;   // 空配列で取り込みを止め、送った予定も消す
  syncCalendar(): Promise<{ sent: boolean; count: number; reason?: 'denied' | 'none_selected' | 'offline' }>;

  addListener(event: 'statusChange', cb: (s: ProximityStatus) => void): Promise<PluginListenerHandle>;
  addListener(event: 'friendNearby', cb: (e: { did: string; lastSeenAt: string; rssi: number }) => void): Promise<PluginListenerHandle>;
  addListener(event: 'sessionExpired', cb: () => void): Promise<PluginListenerHandle>;
}

export interface PermissionStatus {
  bluetooth: PermissionState;      // BLUETOOTH_SCAN と BLUETOOTH_ADVERTISE の両方
  location: PermissionState;       // ACCESS_FINE_LOCATION
  notifications: PermissionState;  // POST_NOTIFICATIONS（Android 13+）
  calendar: PermissionState;       // READ_CALENDAR
}

export interface ProximityStatus {
  running: boolean;
  bluetoothEnabled: boolean;
  advertising: boolean;
  scanning: boolean;
  tokensValidUntil: string | null;
  pendingSightings: number;
  lastUploadAt: string | null;
  friendBeaconsValidUntil: string | null;
  clockOffsetMs: number;           // §10
  calendarSyncedAt: string | null; // §11
  lastError: null | 'permission_revoked' | 'bluetooth_off' | 'advertise_failed' | 'scan_failed' | 'tokens_exhausted' | 'session_expired';
}
```

- API のベース URL は `capacitor.config.ts` のプラグイン設定 `ChainProximity.apiBase`（開発時は `https://chain.i32.jp`）
- native の HTTP クライアントは OkHttp。すべての呼び出しで `Authorization: Bearer` に native が持つ access token を付ける

## 3. セッション

backend 03 §3 のトークンは **native だけが保持する**。WebView は refresh token を持たない。

```
1. WebView: Browser.open(`${apiBase}/auth/login`)
2. backend → chain://auth/callback?code=<otc>（App プラグインの appUrlOpen で WebView が受ける）
3. WebView: ChainProximity.completeLogin({ code })
4. native: POST /api/v1/auth/exchange → access / refresh を EncryptedSharedPreferences（Android Keystore の鍵）に保存
5. WebView: API を呼ぶ前に getAccessToken() で取得し、期限まで手元に置く
```

- `getAccessToken` は、期限まで 60 秒を切っていれば native が `POST /auth/refresh` してから返す。refresh は native 内で 1 本に直列化する（同時に 2 回ローテーションすると、backend が再利用とみなしてセッション系列ごと失効させるため）
- refresh が `401` なら、保存しているトークンを消し、`sessionExpired` を発火して FGS を止める
- `logout` は `POST /auth/logout` を呼んでからトークンを消し、FGS を止める

## 4. 権限

| 権限 | 種類 | 用途 | 優先度 |
| --- | --- | --- | --- |
| `BLUETOOTH_SCAN` | runtime | scan | P0 |
| `BLUETOOTH_ADVERTISE` | runtime | advertise | P0 |
| `ACCESS_FINE_LOCATION` | runtime | グループの geofence 判定、フレンドの位置共有 | P0 |
| `POST_NOTIFICATIONS` | runtime（13+） | FGS の通知、プッシュ | P0 |
| `FOREGROUND_SERVICE`、`FOREGROUND_SERVICE_CONNECTED_DEVICE` | install | FGS（type `connectedDevice`） | P0 |
| `FOREGROUND_SERVICE_LOCATION` | install | FGS 内での位置取得（type `location`） | P1 |
| `ACCESS_BACKGROUND_LOCATION` | runtime | バックグラウンドでの位置共有・入場通知 | 後回し（backend README L2） |
| `READ_CALENDAR` | runtime | 自動グループのための今日・明日の予定の読み取り（§11） | P2（自動グループと同時） |

- `BLUETOOTH_SCAN` には **`neverForLocation` を付けない**。付けると一部の beacon 型の advertise が scan 結果から除かれ得る（research E2）。位置権限はグループのために結局取るので、付けない方を選ぶ
- `BLUETOOTH_CONNECT` は使わない（GATT 接続をしない）
- 権限の要求は機能を使う直前に、理由の説明画面を挟んでから行う（審査対策、research §3.1）
- minSdk 26（`AdvertisingSet` API とチャンネル付き通知のため）、targetSdk は Capacitor の既定に合わせる

## 5. Foreground Service

- `start()` は前面からだけ呼べる。Android 12 以降は、バックグラウンドから FGS を起動できないため
- FGS の type は `connectedDevice`（P1 で `connectedDevice|location`）
- 常駐通知: チャンネル `proximity`（重要度 low）。本文「すれ違いモード: オン」、操作「停止」
- ユーザーが `start()` した状態を保存しておき、端末の再起動後（`BOOT_COMPLETED`）に再開する。Android 15 で再起動時の FGS 起動の制限が `connectedDevice` に及ぶかは実機で確認する（research U1）
- Bluetooth がオフになったら advertise と scan を止めて `lastError = 'bluetooth_off'`。オンに戻ったら（`ACTION_STATE_CHANGED`）再開する
- 権限が取り消されたら FGS を止め、`permission_revoked` を通知する

## 6. advertise

| 項目 | 値 |
| --- | --- |
| API | `BluetoothLeAdvertiser.startAdvertisingSet`（legacy mode、non-connectable、non-scannable） |
| 間隔 | `INTERVAL_MEDIUM`（約 250 ms）。歩いてすれ違う程度の短い接近でも拾えるようにするため。電池を実測して調整する（research U4） |
| 送信出力 | `TX_POWER_MEDIUM` |
| データ | backend 05 §2.3 の 26 バイト。`includeDeviceName = false`、`includeTxPowerLevel = false`（端末名を出さない） |
| token の切替 | 窓の境界（`window_start + 900 秒`、§10 の時刻補正後）で advertising set を作り直す。作り直すと送信元アドレスも変わることを期待する（実機で確認） |
| token がない | advertise を止め、scan だけを続ける（`tokens_exhausted`）。古い token は再利用しない |

## 7. scan と sighting の集約

| 項目 | 値 |
| --- | --- |
| ScanFilter | `setManufacturerData(0xFFFF, [0x43, 0x48, 0x01], [0xFF, 0xFF, 0xFF])`（magic と version） |
| ScanSettings | 画面オン: `SCAN_MODE_BALANCED`、画面オフ: `SCAN_MODE_LOW_POWER`。`CALLBACK_TYPE_ALL_MATCHES`、`reportDelay = 0` |
| 再起動 | 25 分ごとに scan を止めて再開する（30 分を超える scan は OS に opportunistic に格下げされるため）。`startScan` は 30 秒に 5 回までという OS の制限を超えない |
| フィルタなし scan | しない（画面オフで止められるため） |

scan 結果ごとに次を行う。

```
token = manufacturerData[3..19]
if token が friend beacon 表にある → §8 の Party 処理へ（sighting にも入れる）
agg[token].first = min(first, now); last = now; maxRssi = max(maxRssi, rssi); count++
```

- `agg` は端末内の SQLite（Room）に保存し、プロセスが落ちても失わない
- 送信は backend 06 §2 のとおり（5 分ごと、50 件超、前面に来た時、`flushSightings()`）。成功した分だけ消す
- 送信に失敗したら指数バックオフ（30 秒から、上限 10 分）。`last` が 24 時間を超えたものは送らずに消す

## 8. フレンドの近接と Party 報告

- `GET /party/friend-beacons` で得た token → DID の表を、暗号化した端末内ストレージに置く。3 時間ごとと `refreshFriendBeacons()` で取り直す
- フレンドを観測したら `friendNearby` を発火する（同じ相手については 15 秒に 1 回まで）
- 直近 45 秒以内に観測したフレンドが 1 人以上いる間は、15 秒ごとに `POST /party/presence` を送る。全員いなくなったら空配列を 1 回送る（backend 08 §2）
- Party の状態そのもの（仮 / 本、メンバー）は native で判定しない。backend の `party.updated` を WebView が受けて表示する

## 9. 位置

- 取得は Fused Location Provider。`isMock` は `Location.isMock()`（API 31+）または `isFromMockProvider()` で取る
- 前面: `setForegroundLocation({enabled: true})` で 30 秒ごとに `PUT /me/location` を送る（backend 05 §4）。精度が 200 m より悪い、またはモックの位置は送らない
- グループの作成・参加の直前には、WebView が `updateLocationNow()` を呼んでから backend を呼ぶ（backend の鮮度条件が 120 秒のため）
- バックグラウンドの位置送信は後回し（backend README L2）

## 10. 時刻の補正

beacon の窓の選択と、sighting の時刻は backend の時計を基準にする（許容幅は ±120 秒）。

- backend の応答の `Date` ヘッダと、往復時間の半分から、端末時計とのずれ `clockOffsetMs` を求める。直近 5 回の中央値を使う
- 窓の切替時刻と、送信するすべての時刻に `clockOffsetMs` を足す
- ずれが 60 秒を超えたらログに残す（端末の時計設定がおかしい可能性があるため）

## 11. カレンダーの取り込み

自動グループ（backend 09 §8）の「同じ予定の人」を判定するために、端末のカレンダーから今日と明日の予定を読んで送る。
Google Calendar API は使わない（Google の審査が重い権限になるため）。端末に同期済みの Google カレンダーなども、端末のカレンダーとして読める。

### 11.1 始め方

1. ユーザーが設定画面で「自動グループ」をオンにしたときに、理由の説明画面を挟んでから `READ_CALENDAR` を要求する
2. `listCalendars()` で端末のカレンダー（`CalendarContract.Calendars`）を一覧にし、**読むカレンダーをユーザーに選ばせる**。既定ではどれも選ばない（仕事用など、送りたくないカレンダーを除けるように）
3. `setSelectedCalendars({ids})` で保存する（暗号化した端末内ストレージ）。保存したらすぐに 1 回同期する

### 11.2 読み方

- `CalendarContract.Instances` で、選んだカレンダーの今日と明日（Asia/Tokyo）の予定を読む。繰り返しの予定も個別の回として取れる
- 読む列は**タイトル・開始・終了だけ**。説明・場所・参加者は読まない
- 次のものは送らない
  - 終日の予定（`ALL_DAY = 1`）
  - 自分が「不参加」にした予定（`SELF_ATTENDEE_STATUS = DECLINED`）
  - 10 分未満、または 12 時間を超える予定
  - タイトルが空の予定
- 日をまたぐ予定は、その日の 0:00〜24:00 に切り詰める。時刻は §10 の補正をかけた Asia/Tokyo の `HH:MM` にする
- タイトルは 100 文字、1 日 20 件に切り詰める（開始の早い順）（backend 09 §8.2 の上限）

### 11.3 送り方

- `PUT /api/v1/me/schedule-today` を今日と明日の 2 回、`source: "calendar"` で送る。同じ source の予定だけが置き換わるので、手入力の予定（`source: "manual"`）は消えない
- 前回送った内容と同じなら送らない（タイトル・時刻のハッシュを端末に持つ）

| 契機 | 同期 |
| --- | --- |
| アプリが前面に来た（WebView が `syncCalendar()` を呼ぶ） | する |
| FGS の稼働中、3 時間ごと | する |
| 日付が変わった直後（0:05 JST） | する（明日だった分が今日になる） |
| カレンダーの変更（`ContentObserver` で `CalendarContract.Events` を見張る） | 5 分まとめてから同期する |
| `setSelectedCalendars({ids: []})`、権限の取り消し、ログアウト | 今日と明日の `source: "calendar"` を空の `entries` で送り、送った予定を消す |

- オフラインや 5xx は、次の契機まで待つ（再送の待ち行列は持たない。予定は次の同期で必ず最新になるため）
- 予定のタイトルを平文で送ることは、backend README L8 の課題として残っている

## 12. 未検証点（実機で確かめる）

| # | 内容 | 関連 |
| --- | --- | --- |
| V1 | 各社端末で FGS が止められずに advertise / scan が続くか | research U1 |
| V2 | advertising set の作り直しで送信元アドレスが変わるか | §6 |
| V3 | `INTERVAL_MEDIUM` と `SCAN_MODE_BALANCED` での発見までの時間、届く距離、1 時間あたりの電池消費 | research U4 |
| V4 | 画面オフ時に、manufacturer data のフィルタ付き scan で結果が届き続けるか | §7 |
| V5 | 再起動後の FGS 再開が Android 15 以降で許されるか | §5 |
| V6 | 各社のカレンダーアプリ（Samsung カレンダーなど）の予定が `CalendarContract` から同じように読めるか。カレンダーの変更の `ContentObserver` が FGS の中で届くか | §11 |
