# 08 Party

## 1. 定義

- フレンド同士が BLE で近くにいる状態が続くと、自動で作られる一時的な Space（`jp.i32.chain.space.party`）
- 30 秒続くと**仮 party**（同じ空間にいるかを確かめている段階）、5 分続くと**本 party** になる（draft）
- 1 人が同時に所属できる party は 1 つ。フレンドのフレンドも、同じ party に入る（draft「フレンドのフレンドなどはそのpartyに分属される」）
- 本 party の間に撮った写真は、設定でオンにしていれば自動で共有される（§5）。終了した party は archive され、memory（13）から見られる

## 2. フレンドの近接検出（P0）

すれ違いの sighting（5 分ごとのまとめ送信）では、30 秒という粒度に追いつけない。
そこで、フレンドの beacon token を事前に端末へ配り、端末側でフレンドを見分けて短い間隔で報告させる。

`GET /api/v1/party/friend-beacons`

```json
{
  "validUntil": "2026-10-17T09:00:00Z",
  "friends": [
    { "did": "did:plc:...", "tokens": [ { "windowStart": "2026-10-17T03:00:00Z", "token": "base64url" } ] }
  ]
}
```

- 現在から 6 時間分。対象は、相手が `friend_beacon_share = 1` かつ `party_auto = 1` のフレンドだけ
- 相手の token を事前に生成する必要がある場合は、ここで生成する（05 §2.2 と同じ規則）
- クライアントは 3 時間ごとと、フレンドが増減したとき（WebSocket の `friend.changed`）に再取得する
- **トレードオフ**: フレンドはいつでも BLE で自分を識別できるようになる。`friend_beacon_share` を 0 にすれば配られなくなる（その場合、自分は自動 party の対象外になる）

`POST /api/v1/party/presence`

```json
{ "observations": [ { "did": "did:plc:...", "lastSeenAt": "2026-10-17T03:12:40Z", "rssi": -60 } ] }
```

- フレンドが 1 人以上見えている間は 15 秒ごとに送る。全員見えなくなったら空配列を 1 回送る
- フレンドでない DID、ブロック関係の DID は無視する
- 各項目について `PeerProximity.Observe` と §3 のエッジ更新を行う

## 3. 近接エッジ

フレンドの組 `(a, b)`（a < b）ごとに、近接の連続性を Redis に持つ。

```
on observation(u, v, t):
  key = pe:{min(u,v)}:{max(u,v)}
  if t - key.last > EdgeGapReset (60s): key.since = t
  key.last = max(key.last, t)
  ZADD pe:active "{a}|{b}" key.last
```

- どちらか一方向の観測があればエッジは生きている（`now - last <= EdgeAlive` 45 秒）。party はフレンド同士の機能なので、すれ違いほど厳密な双方向性は求めない
- `now - since >= TentativeAfter`（30 秒）のエッジを**安定エッジ**と呼ぶ

## 4. party エンジン（`party.Engine`、5 秒 tick）

### 4.1 tick の手順

```
1. pe:active から now - EdgeAlive 以降のエッジを読み、グラフ G を作る
   （party_auto = 0 のユーザー、ブロック関係の組は除く。明示的に退出した人と、その party のメンバーとの組も除く）
2. 安定エッジだけのグラフ S の連結成分を求める
3. 成分 C ごとに:
   P = C のメンバーが現在所属している party の集合（party:of:{did}）
   - P が空 → 仮 party を作成し、C 全員を入れる
   - P が 1 つ → C のうち未所属の人を P に入れる
   - P が複数 → §4.3 の統合規則
4. 各 party について、在席メンバー（G 上で未離脱の他メンバーへの生きたエッジを持つ人）を数え、§4.2 で状態を進める
5. 状態が変わった party の WebSocket チャンネルとメンバーのユーザーチャンネルに party.updated を送る（終了・破棄も含む。10 §7.3）
```

1 tick は `lock:party-engine` を取って実行する（多重起動の防止）。

### 4.2 状態遷移

```
            30 秒の安定エッジ              仮のまま 5 分、在席 ≥ 2
 (なし) ────────────────▶ tentative ─────────────────────────▶ confirmed
                              │ 在席 < 2 が 60 秒                │
                              ▼                                  │ 在席 < 2 が 10 分（locked でない）
                          (破棄)                                 │ locked: 72 時間 在席も投稿もない / 全員が退出
                                                                 ▼
                                                               ended → space は archived
```

| 遷移 | 条件 | 処理 |
| --- | --- | --- |
| → tentative | §4.1 の 3 | `spaces`（state=active, name=「M/D HH:mm の party」）と `parties`（phase=tentative）を作る |
| tentative → 破棄 | 在席が 2 人未満の状態が `TentativeDissolve`（60 秒）続いた | 行ごと削除する（投稿はできないので消えるものはない）。削除前のメンバーに `party.updated`（`phase: "dissolved"`）を送る |
| tentative → confirmed | `now - tentative_at >= ConfirmAfter`（5 分）かつ在席が 2 人以上 | `confirmed_at` を立て、メンバーに `party.confirmed` を通知する（P1） |
| メンバーの離脱（confirmed） | そのメンバーが `LeaveAfter`（10 分）在席しない。locked なら離脱させない | `left_at` を立て、`can_write = 0`。`can_read` は 1 のまま（archive を読めるように）。`party:of` を消す（未所属に戻る） |
| メンバーの再合流 | 離脱したメンバーが active な同じ party に戻ってきた（未離脱のメンバーへの生きたエッジがある）。ただし、すでに別の party に所属している人は戻さない | `left_at` を消し、`can_write = 1`、`party:of` を戻す |
| confirmed → ended | locked でない: 在席 2 人未満が `LeaveAfter` 続いた。locked: `LockedIdleEnd`（72 時間）在席も投稿もない、または未離脱のメンバーが 2 人未満になった。または開始から `MaxDuration`（7 日） | `ended_at`、`spaces.state = archived`、`party:of:*` を消す |

- 仮 party の間は投稿できない（`write` のアクセス判定で拒否する）。経過時間（`tentativeAt` / `confirmedAt`）は見える
- 明示的な退出: `POST /api/v1/parties/{id}/leave`。退出したメンバーは、その party が終わるまで自動では再合流しない
  - 退出すると `party:of` を消す。その party のメンバーとのエッジは §4.1 の 1 で除くので、元のメンバーの近くにいても、別のフレンドとは新しい party を組める

### 4.3 統合規則

draft の「party は統一的で複数には属せない」を守るための規則。

| 状況 | 規則 |
| --- | --- |
| 仮 party 同士 | 最も古い仮 party に統合する |
| 仮 party と本 party | 仮 party のメンバーを本 party に移し、仮 party を破棄する |
| 本 party 同士 | **統合しない**（README Q6）。未所属の人は、安定エッジが最も古い相手の party に入る |

### 4.4 固定（lock、P1）

draft の論点「旅行ではぐれた瞬間に解散されるのは問題」への対応。

- `POST /api/v1/parties/{id}/lock`、`/unlock`: confirmed の party のメンバーなら誰でも切り替えられる。切り替えは system 投稿として残す
- locked の間は、在席しないことによる離脱と終了が起きない
- locked の party にいるユーザーが別のフレンドと近接した場合も、§4.1 の規則で同じ party に入る
- closed グループとの違い: party は終了すると archive される一時的な記録である。lock は終了を延ばすだけで、場所にも結び付かない

## 5. 写真の端末保存（P0）

- 写真の自動共有（カメラの写真の監視と自動アップロード）は廃止した。party への写真は compose から手動で共有する（`POST /instances/{id}/media`）
- 端末保存は設定でオンにしたときだけ動く。**既定はオフ**で、端末ごとの設定（localStorage。backend の settings には入れない）。設定は `/settings/` の「パーティの写真」にある
- 写真の読み取り権限は使わない

### 5.1 自動ダウンロード

- 同じ party の他のメンバーが共有した写真を、端末のアルバム `Pictures/CHAIN/{party 名}` に自動保存する（Android 10 以降。9 以下は対象外）
- 保存の契機は、party 画面を開いたとき、その Space の `media.created` を受けたとき、`party.updated` の `ended` を受けたとき。前面の間だけ動く
- 保存するのは full 版（長辺 2048）。署名付き URL は 1 時間で切れるので、保存の直前に取り直す
- 自分が上げた写真と、moderation で非表示の写真は保存しない。保存済みは media id で端末に記録し、アルバムから消された写真は再保存しない
- party 終了の Notice に「写真を保存」を付ける。ダウンロード設定がオフでも、ここから手動で保存できる
- 設定を途中でオフにしたら以降の保存を止める。保存済みの写真は消さない

### 5.2 サーバの確認

- 通常は active / confirmed 中の `write` 権限だけを確認する。`taken_at` は表示の並び順と、archived 後の例外の判定に使う
- archived 後 24 時間以内は、終了時に未離脱だったメンバーに限り、`taken_at` が `confirmed_at` 以上 `ended_at` 以下の写真だけを共有できる。この例外は写真共有 API の経路だけに適用し、通常の `write` 判定は変更しない（10 §5）
- native の詳細は android README §3.1

## 6. API

| メソッド | パス | 内容 |
| --- | --- | --- |
| GET | `/api/v1/party/current` | 所属中の party（なければ `null`）。phase、メンバーと在席状態、`tentativeAt`、`confirmedAt`、`locked` |
| GET | `/api/v1/party/friend-beacons` | §2 |
| POST | `/api/v1/party/presence` | §2 |
| POST | `/api/v1/parties/{id}/leave` | 退出 |
| POST | `/api/v1/parties/{id}/lock`、`/unlock` | §4.4 |
| PATCH | `/api/v1/parties/{id}` | `{ "name": "..." }`。confirmed のメンバーのみ。moderation の対象 |

投稿・写真の閲覧と作成は instance の API（10）を使う。

## 7. パラメータ

| 名前 | 既定 | 意味 |
| --- | --- | --- |
| `PresenceInterval` | 15 秒 | クライアントの報告間隔 |
| `EdgeAlive` | 45 秒 | エッジが生きているとみなす最終観測からの時間 |
| `EdgeGapReset` | 60 秒 | 連続性をリセットする観測の途切れ |
| `TentativeAfter` | 30 秒 | 仮 party になる連続時間 |
| `ConfirmAfter` | 5 分 | 本 party になる時間 |
| `TentativeDissolve` | 60 秒 | 仮 party を破棄する不在時間 |
| `LeaveAfter` | 10 分 | 本 party から離脱とみなす不在時間 |
| `LockedIdleEnd` | 72 時間 | locked の party を終了する無活動時間 |
| `MaxDuration` | 7 日 | party の最長期間 |
| `MaxMembers` | 50 | これを超える成分は新規メンバーを入れない |
| `FriendBeaconHorizon` | 6 時間 | 配布するフレンド token の範囲 |
