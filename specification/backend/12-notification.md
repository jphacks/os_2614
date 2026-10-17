# 12 通知

## 1. 経路

- プッシュは FCM（Firebase Admin SDK の HTTP v1）。Capacitor の Push Notifications プラグインが FCM トークンを取る
- アプリ内の通知一覧（`notifications` 表）にも同じものを残す。前面にいる端末には WebSocket の `notification` イベントでも送る

## 2. 端末の登録

- `PUT /api/v1/me/devices {"fcmToken": "...", "platform": "android"}`: アプリ起動時と、FCM トークンが更新された時に呼ぶ。`fcm_token` が既に別ユーザーに紐付いていれば付け替える（端末の持ち主が替わった場合）
- `DELETE /api/v1/me/devices/{fcmToken}`: ログアウト時
- FCM が `UNREGISTERED` を返したトークンは削除する

## 3. 通知の種類

| type | 契機 | 既定 | 本文の例 | 優先度 |
| --- | --- | --- | --- | --- |
| `friend.request` | フレンド申請を受けた | on | 「Bob さんからフレンド申請」 | P0 |
| `friend.accepted` | 申請が承認された / 招待コードが使われた | on | 「Bob さんとフレンドになりました」 | P0 |
| `party.confirmed` | 本 party になった | on | 「Bob さんたちと party が始まりました」 | P1 |
| `group.nearby` | open グループのエリアに入った | on | 「近くに『情報科学演習 A』があります」 | P1 |
| `group.invited` | closed グループの招待が承認数に達した | on | 「『〇〇』に招待されました」 | P1 |
| `group.invite_approval` | closed グループで、他のメンバーが招待を作った（承認のお願い） | on | 「Bob さんが『〇〇』に Carol さんを招待しようとしています」 | P1 |
| `group.auto_joined` | 自動グループに参加させた（09 §8.5） | on | 「『線形代数 II』のグループに参加しました」 | P2 |
| `group.vote_passed` | グループの投票が可決した（09 §8.6） | on | 「『〇〇』は招待制になりました」 | P2 |
| `encounter.daily` | 毎日 21:00 JST | on | 「今日のすれ違い: 12 人」 | P1 |
| `encounter.weekly` | 毎週月曜 21:00 JST（前週分） | on | 「先週のすれ違い: 48 人」 | P1 |
| `encounter.monthly` | 毎月 1 日 21:00 JST（前月分） | on | 「10 月のすれ違い: 160 人」 | P1 |
| `moderation.hidden` | 後からの判定で投稿が非表示になった | on（切れない） | 「投稿が非表示になりました」 | P1 |

- 設定: `PATCH /api/v1/me/settings {"notify": {"group.nearby": false}}`。`user_settings.notify_json` に、既定から変えた種類だけを保存する
- **すれ違いの通知には相手の名前を入れない**。回数だけにする（公開遅延を破らないため）。ダイジェストの回数は 06 §6 の summary と同じ規則で数え、0 件なら送らない
- Android の通知チャンネルは `social`（フレンド・party・グループ）と `digest`（すれ違い）の 2 つに分け、ユーザーが OS 側で切り分けられるようにする。FGS の常駐通知は native の `proximity` チャンネル（`specification/android` §5）

## 4. 送信の仕組み

```
業務処理 ──(同じトランザクション)──▶ notifications ＋ notification_outbox(queued)
                                              │
notify.Sender（2 秒ごと）───────────────────────┘
  ├─ due な outbox を 100 件取得
  ├─ 宛先ユーザーの全 devices に FCM 送信
  ├─ 成功 → sent
  └─ 失敗 → attempts++、next_attempt_at = now + 2^attempts × 10 秒。5 回失敗で failed
```

- 通知の生成を業務処理と同じトランザクションに入れることで、「処理は成功したが通知が消えた」を防ぐ
- FCM のメッセージは `notification`（タイトルと本文）と `data`（`type` と、遷移先の ID）を両方入れる
- `collapse_key` は、ダイジェストは `type` ごと、`group.nearby` は Space ごとにする

## 5. ダイジェスト（`notify.Digest`）

- 21:00 JST に起動し、対象のユーザーを 500 件ずつに分けて回数を計算し、outbox に積む
- 重複を防ぐため、`notifications` に `(did, type, 対象日)` が既にあれば積まない（`payload_json` の `period` で判定する）
- 再起動などで 21:00 を逃した場合、その日の 23:59 までに起動すれば積む

## 6. 未決・P2

- 時間帯による通知の抑止（深夜は送らない）
- `group.nearby` の頻度（現状は同じグループについて 24 時間に 1 回）を実機で調整する
