# Party 実装仕様（08 の補足）

`specification/backend/08-party.md`（以下 08）を、いまのコードに合わせて実装できる粒度まで具体化する。
08・10・13・`docs/spec/ui-v2.md` §4.5・`specification/android` §8 と矛盾しないことを前提にし、足りない決めごとだけをここで足す。
既存仕様書の変更が要るところは §8 にまとめる。返信の見せ方と写真の端末保存は §11 を正とする（§6.2 の返信・写真の記述より優先する）。

## 1. 前提（いまの構成）

| 層 | 実際の構成 | 仕様との差 |
| --- | --- | --- |
| native（`web/android/.../proximity`） | advertise / scan だけを持つ。受信のたびに `sighting` イベント（`token`、`rssi`、`seenAt`。同じ token は 5 秒に 1 回まで）を JS に送る | android §2 の `refreshFriendBeacons` / `friendNearby` は無い |
| JS（`web/src/device/proximity`） | token の取得、sighting の送信、位置の送信など、backend との通信はすべて JS が持つ（`definitions.ts` の冒頭、`auth/session.ts` の注記） | android §1 では native が持つ前提 |
| backend | 08 の検出・判定処理・API はすべて実装済み（`service/party/party.go`、`worker/party_engine.go`） | — |

この構成に合わせ、Party の presence 送信も JS 側に置く（§5）。

## 2. 検証結果

`make test-integration` と同じ環境（Redis 実体 + `partyHarness`）で一時的なシナリオテストを書いて確かめた（テストは削除済み。§7 で正式に足す）。

| # | 内容 | 結果 | 根拠 |
| --- | --- | --- | --- |
| V1 | A・B・C の本 party から C が**明示的に退出**し、その後 C が D（C だけのフレンド）と 6 分いっしょにいる | **D が A・B の party に入れられる**。C 本人はどの party にも入らない（`Current(C) = nil`） | 再現済み |
| V2 | 同じ構成で C が **10 分不在で自然に離脱**した後、D と会う | **C と D が A・B の party に合流する**（A・B から遠く離れているのに） | 再現済み |
| V3 | 仮 party が破棄される（`dissolveParty`、統合されない場合） | `party.updated` も他のイベントも送られない | コード（`party.go` `dissolveParty`） |
| V4 | 本 party が終了する（`endParty`） | `party.updated` は `Members: []` で作られるため、**メンバーのユーザーチャンネルには何も届かない**。Space チャンネルにだけ届くが、`/party` 画面は Space を購読していない | コード（`endParty`、`publishPartyUpdated`） |
| V5 | `party.updated` の data | party の ID が無い（`phase`、`locked`、`present` だけ）。ユーザーチャンネルで受けると、どの party の更新か区別できない | コード |
| V6 | `/party` 画面が「party 無し」のときに `party.updated` を受ける | 取得結果が `null` のときは重ねないので、**新しくできた party が再読み込みするまで表示されない** | コード（`app/party/page.tsx`） |
| V7 | `GET /instances` | 仮 party と、離脱済みの party も返す（`left_at` を見ていない）。compose は `archivedAt` だけで絞るので、**書けない送信先が出る** | コード（`ListUserSpaces`、`compose/page.tsx`） |
| V8 | `/party` 画面の投稿 | `media` だけを抜き出し、本文の投稿は表示しない。しかも 1 ページ目しか読まない | コード |
| V9 | 仮 party の画面 | 投稿・固定・改名のボタンが出ている。backend はすべて拒否する（10 §5、08 §4.4・§6） | コード |
| V10 | フレンドを解除する（`DELETE /friends/:did`）・ブロックする | 配った 6 時間分の token を作り直さない。**相手は最大 6 時間、手元の表で BLE からこちらを見分け続けられる** | コード（`friend`・`block` に token の処理が無い） |
| V11 | 自分が `friend_beacon_share = 0` | 自分の token は配られないが、**自分は他人の表を受け取れ、presence も送れる**。08 §2 の「自分は自動 party の対象外になる」が守られていない | コード（`FriendBeacons`、`Presence`、判定処理は `party_auto` だけを見る） |
| V12 | `POST /party/presence` | 「この DID を見た」という申告をそのまま信じる。08 §3 は片方向の観測でエッジを生かすので、**フレンドは遠くからでも B の party に合流でき、B のフレンドのフレンドの写真・投稿まで読める** | コード（`Presence`） |

V1・V2 の原因は 1 つで、**離脱しても `party:of:{did}` が消えない**ことにある。

- 04 の Redis キー表では `party:of:{did}` は「所属中の party」だが、`Leave`（明示的な退出）と `advanceConfirmed`（不在による離脱）はこれを消していない。`RebuildIndex` も離脱済みのメンバーを含めて組み直す
- その結果、判定処理は離脱した人を「まだその party にいる人」として扱い（08 §4.1 の 3）、その人と一緒にいる未所属のフレンドを元の party に入れてしまう
- V1 の D は A・B と面識が無いのに、A・B の party の投稿と写真を読めるようになる。**プライバシー上の不具合**として最優先で直す

V10〜V12 は beacon 表と presence の秘匿性の問題で、§3.3 で扱う。いずれもコードを読んで確かめたもので、テストでは再現していない。

beacon 表そのものは、次の点では担保されている（`FriendBeacons`、05 §2.1）。

- 配る相手はフレンド関係（`friendships`）にある相手だけ。削除済みのアカウント、どちら向きでもブロック関係にある相手、`friend_beacon_share = 0` か `party_auto = 0` の相手は除く
- token は 16 バイトの暗号学的乱数で、15 分ごとに変わる。DID から作っていないので、token から本人は割り出せない
- サーバーには `SHA-256` のハッシュと、AES-256-GCM で暗号化した値だけを保存する

## 3. 所属と秘匿性（backend）

### 3.1 不変条件

> `party:of:{did}` が `S` を指すのは、`S` が active な party で、`did` が `S` の**未離脱**のメンバーのときだけ。

| 処理 | いま | 直す内容 |
| --- | --- | --- |
| `Leave`（明示的な退出） | `left_at` と `pleft:{space}` に記録 | `ClearPartyOf(did)` を足す |
| `advanceConfirmed` の不在による離脱 | `LeavePartyMember` だけ | `ClearPartyOf(did)` を足す |
| `advanceConfirmed` の再合流 | `RejoinSpaceMember` だけ | `SetPartyOf(did, S)` を足す |
| `addMembers` | 退出した人（`pleft:{space}`）でも `RejoinSpaceMember` してしまう | `PartyLeavers(S)` に含まれる DID は飛ばす（08 §4.2「退出したメンバーは、その party が終わるまで自動では再合流しない」） |
| `RebuildIndex` / `ListActivePartyMembers` | 離脱済みも含める | `m.left_at IS NULL` を条件に足す |
| `endParty` / `dissolveParty` | 全員の `party:of` を消す | そのまま |

### 3.2 直した後の動き

- 離脱した人は「未所属」になる。次の tick から、08 §4.1 の規則でそのまま扱われる
  - 元の party の未離脱メンバーと安定エッジがある（= 戻ってきた） → 成分に元の party が入るので、未所属の人として元の party に割り当てられ、`addMembers` で再合流する（08 §4.2「メンバーの再合流」）
  - 元の party と無関係のフレンドとだけ近い → 新しい仮 party ができる
- 明示的に退出した人と、その party のメンバー（離脱済みを含む）との間のエッジは、tick でグラフを作るときに捨てる。退出者は元のメンバーとは「近くにいない」扱いになる
  - これがないと、退出直後の 45 秒（エッジが生きている間）に退出者が橋になり、退出者のフレンドが元の party に入れられる（実装中に P11 で確認）
  - 退出者は元のメンバーの近くにいても、別のフレンドとなら新しい party を組める。`addMembers` でも退出者を飛ばす（二重の守り）
- 不在で離脱した人を再合流させるのは、`party:of` が空のときだけ。別の party に入った後で元のメンバーに近づいても、二重には所属しない
- `present`（在席）は 08 §4.1 の 4 のとおり「G 上で**未離脱の**他メンバーへの生きたエッジを持つ人」と読む。離脱済みメンバー同士のエッジで在席にならないようにする

### 3.3 beacon 表と presence の秘匿性

| # | 直す内容 | 仕様の根拠 |
| --- | --- | --- |
| V10 | フレンドの解除・ブロックのときに、**自分の token のうち今の窓とそれ以降を作り直す**（`beacon_tokens` の行を消して新しく作る）。自分の端末には新しいイベント `beacon.rotated`（ユーザーチャンネル、data なし）で知らせ、`GET /beacon/tokens` を取り直させて発信を差し替える。WebSocket が切れていた場合は、前面に戻ったときの token の再取得（`shouldRefreshTokens`）では拾えないので、再接続後にも 1 回取り直す。相手の手元の表は、次の窓から役に立たなくなる | 05 §2「ユーザー ID を周囲にばら撒かない」、README L5（つきまとい対策） |
| V11 | 本人が `friend_beacon_share = 0` のときは、`GET /party/friend-beacons` は空の `friends` を返し、`POST /party/presence` の観測は捨てる。判定処理も、`party_auto = 0` と同じく除く | 08 §2「その場合、自分は自動 party の対象外になる」 |
| V12 | 本人の判断が要る（§9 の D5）。候補は (a) 新しく party に入るときだけ両方向の観測を求める、(b) 「フレンドのフレンド」として自動で入れるのは、既存メンバーとの両方向の観測があるときだけにする | 08 §3 は「片方向でよい」としているので、どちらを選んでも 08 §3 を変える |

- V12 は、token を送らせる方式に変えても防げない。フレンドは 6 時間先までの token を表で持っているため、token は「本当に近くで受信した」証明にならない
- V10 の作り直しは、今の窓も含める。今の窓を残すと、解除の直後から最大 15 分は見分けられてしまうため
- V10 の作り直しで自分の発信が切れないように、新しい token を取得して native に渡すまでは古い token で発信を続ける（`setBeaconTokens` で置き換える）

関連して、優先度は低いが残っている点。

- token を 15 分ごとに変えても、BLE の送信元アドレスが変わらなければ他人から同じ端末だと追跡できる。アドレスが変わるかは android §12 の V2 で「実機で確認」のまま
- 自分の token は端末の SharedPreferences に暗号化せずに置いている（`ProximityState.kt`）。抜き取られると別の場所でなりすまして発信されるが、端末の中身を読める相手が前提なので影響は小さい
- すれ違いと party で token が同じなので、フレンドは party に関係なく、こちらが近くにいることをいつでも分かる。これは 08 §2 のトレードオフとして決めたもの

## 4. イベント（backend → 画面）

### 4.1 `party.updated` の data を広げる

```json
{ "id": "<space id>", "phase": "tentative | confirmed | ended | dissolved", "locked": false, "present": ["did:plc:..."] }
```

- `id` を足す（V5）
- `phase` に `dissolved` を足す。仮 party が統合されずに破棄されたときに使う。`parties` の行は消えるので、この値は DB には現れない
- 宛先は 10 §7.3 のとおり「Space ＋ メンバーのユーザー」。ここでいうメンバーは、**その時点の行を消す・終える前のメンバー全員**（離脱済みを含む）とする

### 4.2 送る場面

| 場面 | いま | 直す内容 |
| --- | --- | --- |
| 仮 party の作成・メンバー追加・lock・退出・改名・確定 | 送っている | `id` を足すだけ |
| 本 party の終了（`endParty`） | `Members: []` のためユーザーには届かない（V4） | 終了前の `members` を使ってユーザーチャンネルにも送る |
| 仮 party の破棄（統合なし） | 何も送らない（V3） | 削除前のメンバー全員に `phase: "dissolved"` を送る |
| 仮 party の統合（`mergeInto`） | 統合先の `party.updated` が新しいメンバーにも届く | そのまま。画面は `id` が変わったことで取り直す（§6.4） |

## 5. presence の送信（JS、08 §2 のクライアント側）

### 5.1 置き場所

- 新しいモジュール `web/src/device/proximity/party.ts`。純粋な関数（観測の集約、送るかどうかの判定）と、`PartyPresenceReporter`（タイマーと送信）に分ける。`sightings.ts` / `sync.ts` と同じ分け方にする
- 起動は `useProximityPipeline` から。すれ違いと同じく、signedIn で native が走っている間だけ動かす（`ProximityDriver` が AppShell に 1 つだけマウントされる前提をそのまま使う）

### 5.2 フレンドの beacon 表

| 項目 | 決め |
| --- | --- |
| 取得 | `GET /party/friend-beacons` |
| 取り直す契機 | 起動時、3 時間ごと（08 §2）、`validUntil` まで 30 分を切ったとき、WebSocket の `friend.accepted` / `friend.changed` を受けたとき |
| 持ち方 | `Map<token, did>`。**メモリにだけ置き、localStorage には書かない**（フレンドを BLE で識別できる情報のため。android §8 は暗号化ストレージを求めているが、JS には相当するものが無いので、持たない方を選ぶ）。起動のたびに取り直す |
| 窓 | 応答の全窓の token をまとめて入れる。窓ごとに分けない（受信した token がどの窓かは問わない） |
| 失敗 | 60 秒より短い間隔では再試行しない（`TOKEN_REFRESH_RETRY_MS` と同じ） |

### 5.3 観測と送信

```
on sighting(e):                     // native の "sighting"。5 秒に 1 回まで届く
  did = table.get(e.token); if !did: return
  last[did] = { lastSeenAt: max(last[did].lastSeenAt, e.seenAt), rssi: e.rssi }

every PresenceInterval (15 s):
  obs = [ (did, v) in last | now - v.lastSeenAt <= EdgeAlive (45 s) ]
  if obs non-empty:        POST /party/presence { observations: obs };  sentNonEmpty = true
  else if sentNonEmpty:    POST /party/presence { observations: [] };   sentNonEmpty = false
  45 秒より古い last は消す
```

- 時刻は native の `seenAt` をそのまま使う（`clockOffsetMs` 補正済み）。`now` も同じ補正をかけた時計で比べる
- レート制限は presence バケット（10 秒に 30 回）なので 15 秒間隔なら当たらない。`429` は `Retry-After` を待ち、その間の tick は飛ばす
- 送信の失敗は再送しない。presence は「いま」の情報なので、次の tick が上書きする
- 自分の `party_auto = 0` のときも送る（backend が無視する。設定の読み込みを増やさないため）

### 5.4 前面だけで動くことの制約

`sighting` イベントは WebView が止まっている間は届かず、JS のタイマーも止まる。そのため **アプリが前面にある間しか presence を送れない**。

- 08 §3 ではエッジは**片方向の観測で生きる**ので、近くにいる 2 人のうち 1 人が前面なら party は続く
- 全員が背面に回ると 45 秒でエッジが切れ、仮 party は 60 秒で破棄、本 party は 10 分で離脱になる。lock すれば本 party は終わらない
- android §8 の「バックグラウンドでも 15 秒間隔で送る」は、native に beacon 表と送信を移す P1 として残す（§9 の D2）

## 6. `/party` 画面（web）

### 6.1 状態ごとの表示

`ui-v2.md` §4.5 を土台にし、phase で次のように出し分ける。

| 要素 | party 無し | 仮 party | 本 party |
| --- | --- | --- | --- |
| ヒーロー（経過時間・進捗） | — | 「仮パーティ」、30 秒→5 分の進捗 | 「本パーティ」、確定時刻 |
| 固定トグル | — | 押せない状態で表示し、補足を「本パーティになると固定できます」にする | 操作できる |
| 名前の変更 | — | 出さない | 出す |
| メンバー | — | 出す | 出す（離脱済みは末尾に薄く出す。いまのまま） |
| 写真・タイムライン | — | 出さず、「本パーティになると、写真や投稿を共有できます」を出す | 出す（§6.2） |
| 投稿する（Fab） | — | 出さない | 出す |
| 退出 | — | 出す（backend は仮 party の退出も受け付ける） | 出す |
| 空状態 | 「近くのフレンドと会うとパーティが始まります」 | — | — |

- 仮 party ができたこと・破棄されたことは**通知しない**。通知は本 party になったときの `party.confirmed`（12）だけにする
- 破棄されたら（`phase: "dissolved"`）、画面は空状態に戻る。終了したら（`phase: "ended"`）、空状態に戻し、「パーティが終わりました。メモリーに残っています」を `Notice` で一度だけ出す

### 6.2 タイムライン（本 party）

group のタイムライン（`ui-v2.md` §4.3）と同じ部品を使う。

いま party と group で共有しているもの・していないものは次のとおり。backend はほぼ全部が共通で、party 独自なのは「誰をいつ入れて、いつ終えるか」（`service/party`）だけ。web は compose しか共有していない。

| 部分 | backend | web（group） | web（party） |
| --- | --- | --- | --- |
| Space とメンバー（`model/space`） | 共通 | — | — |
| 投稿・返信・削除（`POST /posts`、`/instances/:id/posts`） | 共通 | 使う | 写真だけを 1 ページ読む |
| 写真の共有（`POST /instances/:id/media`） | 共通 | `SpacePhotoShareScreen`（`/group/photo/?id=`） | compose で写真を付けて投稿する。自動共有は廃止（§11.2） |
| Pin | 共通 | `PinList`、`PinnedPostCard` | 無い |
| 返信 | 共通 | `GroupReplyScreen` / `ReplySheet` | `/party/replies/`（§11.1） |
| リアルタイム（Space チャンネル） | 共通 | 購読する | 購読しない |
| 投稿画面（compose） | — | 使う | 使う（唯一の共有） |
| アクセス判定 | 入口が共通、type で分岐 | 位置の条件 | phase |

group の部品は名前と型が group 前提（`GroupReplyScreen` の props が `Group` 型、ルートが `/group/...`）だが、中で呼ぶ API は instance 共通なので、次のように「Space の部品」として作り直して使う。memory の詳細画面（`docs/spec/memory-implementation.md` §4）も同じ部品で作る。

| 部品 | 作り直し方 |
| --- | --- |
| タイムライン | group 画面のタイムライン部分を切り出し、`readOnly`（memory 用）を持たせる |
| 返信シート | `GroupReplyScreen` を Space 共通にし、ヘッダーを差し替えられるようにする。`ReplySheet` はいま `onSubmit` を渡さないと入力欄を押せない状態で残し、「返信は準備中です」と出す。これとは別に、入力欄ごと隠す `readOnly` を足す |
| 写真の共有 | `SpacePhotoShareScreen` は space ID しか使っていないので、そのまま party からも開く（本 party の間だけ）。ルートは Space 共通の名前に移す |

- 取得は `usePagedResource<InstancePost>("/instances/{id}/posts?roots=1")` の 1 本にする（返信はタイムラインに出さない。§11.1）。写真グリッドは、読み込んだ投稿の `media` から作る（いまは別に 1 ページだけ取っている。V8）
- 並びは「写真グリッド（最大 6 枚、`+n`）」→「タイムライン（`PostCard`、新しい順、続きを読む）」
- lock / unlock の system 投稿（authority 名義、08 §4.4）は `PostCard` ではなく、中央寄せの 1 行（caption muted）で出す
- 返信は instance の P1。返信シートの作りは §11.1 のとおり（背景は party 画面、中身はスレッド API）。ルートは `/party/replies/?id=&post=`
- Pin は party では出さない（Jev の自動 Pin は group 向け。backend は使えるが、画面には載せない）

### 6.3 リアルタイム

- `useRealtimeSubscription(party.id)` で Space を購読する（本 party・仮 party の両方）
- `post.created` / `post.deleted` / `media.created` をタイムラインに反映する（返信の `post.created` は足さず、root のコメント数を +1 する。§11.1）。再接続後は `useRealtimeResync` で取り直す（10 §7.3）

### 6.4 `party.updated` の受け方

```
on party.updated(d):
  if current == null or d.id != current.id: reload GET /party/current   // V6、統合
  else if d.phase in (ended, dissolved):   reload（null が返り空状態になる）
  else:                                     phase / locked / present を重ねる（いまの処理）
```

### 6.5 終わった後（memory）

- 本 party が終わると、Space は archived になり、読めるが書けない状態になる（10 §5。read は離脱済みを含むメンバー、write は `410 gone`。自分の投稿の削除だけはできる）。いいねは API が無い（リアクションは P2）
- `/party` 画面を開いている間に終わったら、§6.4 で空状態に戻し、Notice から memory の詳細画面（`docs/spec/memory-implementation.md` §4、`/memory/detail/?id=`）に移れるようにする
- 開いている間の操作が `410 gone` になったときも、同じく memory の詳細画面に移す

### 6.6 compose の送信先

- `GET /instances` の各要素に `canWrite`（boolean）を足す。backend は `access.Check(..., ModeWrite)` の結果を返す（位置が古いなどで判定できない group は `true` にし、投稿時の判定に任せる）
- compose は `state = active` かつ `canWrite` の送信先だけを出す（V7）。`?target=party:{id}` で書けない party が指定されたら、選択しない状態で開く

## 7. 検証（テスト）

### 7.1 backend（`scenario_test.go` に足す。15 の P 番号を続ける）

| # | 入力 | 期待 |
| --- | --- | --- |
| P11 | A・B・C の本 party から C が明示的に退出し、C と D が 6 分近接 | A・B の party に D は入らない。C・D の新しい party ができる |
| P12 | 同じ構成で、C が 10 分不在で離脱した後、D と近接 | 同上 |
| P13 | C が 10 分不在で離脱した後、A・B のもとに戻る | C が同じ party に再合流し、`party:of:C` が戻る |
| P14 | C が明示的に退出した後、A・B のもとに戻る | C は再合流しない |
| P15 | 仮 party が統合されずに破棄される | メンバー全員のユーザーチャンネルに `party.updated{phase:"dissolved", id}` が届く |
| P16 | 本 party が終了する | メンバー全員（離脱済みを含む）のユーザーチャンネルに `party.updated{phase:"ended", id}` が届く |
| P17 | `RebuildIndex` | 離脱済みのメンバーには `party:of` を作らない |
| P18 | A が B のフレンドを解除する（またはブロックする） | B の token のうち今の窓以降が作り直され、A の `GET /party/friend-beacons` に B が出ない。B のユーザーチャンネルに `beacon.rotated` が届く |
| P19 | A が `friend_beacon_share = 0` | A の `GET /party/friend-beacons` は空。A が送った presence でエッジができない |
| P20 | archived の party | 閲覧はできる。投稿・返信・Pin は `410`。写真の共有も `410` だが、終了から 24 時間以内・終了時に未離脱・期間内の `takenAt` なら許す（10 §5、§11.2）。自分の投稿の削除はできる |
| — | `GET /instances` | 仮 party と離脱済みの party は `canWrite: false` |

### 7.2 web

| 種類 | 内容 |
| --- | --- |
| unit（`party.ts`） | 45 秒の窓、空配列を 1 回だけ送る、知らない token を無視する、`Retry-After` の間は送らない |
| e2e（`party.spec.ts`） | 仮 party で Fab・改名が出ず、固定が押せない／本 party でタイムラインに本文の投稿が出る／`post.created` で増える／`party.updated` の `dissolved`・`ended` で空状態に戻る／`id` の違う `party.updated` で取り直す |
| e2e（compose） | `canWrite: false` の party が送信先に出ない |
| unit / e2e（token） | `beacon.rotated` を受けたら token を取り直して native に渡す。再接続後にも 1 回取り直す |

### 7.3 実機

- 2 台を並べて前面のまま 30 秒 → 仮 party、5 分 → 本 party になる
- 1 台だけ背面にしても続く（§5.4）。両方を背面にすると、仮 party は約 2 分で破棄される

## 8. 既存の仕様書への反映

| 文書 | 箇所 | 内容 |
| --- | --- | --- |
| 04 | Redis キー表 `party:of:{did}` | 「未離脱のメンバーのときだけ」を明記 |
| 08 | §4.1 の 4 | 在席の判定は「未離脱の他メンバーへのエッジ」 |
| 08 | §4.2 の離脱・再合流 | 離脱で `party:of` を消し、再合流で戻す。明示的な退出者は `addMembers` で飛ばす |
| 10 | §7.3 `party.updated` | data に `id`、`phase` に `dissolved`。終了・破棄でもメンバーのユーザーチャンネルに送る |
| 05 | §2.2 | フレンドの解除・ブロックで、今の窓以降の token を作り直す |
| 08 | §2 | `friend_beacon_share = 0` の本人には表を返さず、presence も捨てる |
| 08 | §3 | D5 の決定に合わせて、片方向の観測の扱いを変える |
| 10 | §7.3 | `beacon.rotated`（ユーザー）を足す |
| 13 | §2 `GET /instances` | `canWrite` を足す |
| `ui-v2.md` | §4.5 | §6.1 の出し分け、タイムライン、`/party/replies/` |
| 10 | §4.2・§4.2.1・§4.4・§5・§7.3 | `roots=1`、`replyCount`、スレッド API、写真の重複、終了後 24 時間の例外、返信の `post.created` |
| 08 | §5 | 写真の端末保存（設定、自動保存） |
| android | §3.1・§12 | `ChainPhotos`（端末保存のみ） |
| android | §1・§8 | presence の送信はいまは JS が前面だけで行う。native への移動は P1 |
| 15 | Party の表 | P11〜P20 |

## 9. 決めてほしいこと

| # | 論点 | 本書の仮置き |
| --- | --- | --- |
| D1 | ~~明示的に退出した人が、元のメンバーの近くにいる間は新しい party もできない~~ | 解消。退出者と元メンバーのエッジを捨てる（§3.2） |
| D2 | presence を前面だけで送る（§5.4） | MVP は前面のみ。バックグラウンドは native に移す P1 |
| D3 | 仮 party の間は写真・投稿の欄を隠す（§6.1） | 隠して案内文だけを出す |
| D4 | party の返信を MVP に入れるか（§6.2） | 入れる（group の部品を Space 共通にすれば使えるため） |
| D5 | presence の申告をどこまで信じるか（§3.3 の V12） | (b) 「フレンドのフレンド」として自動で入れるときだけ、既存メンバーとの両方向の観測を求める。フレンド同士の party は片方向のまま |

## 10. 実装の順番

1. §3.1・§3.2（`party:of`）と P11〜P14、P17、§3.3 の V10・V11 と P18・P19。どれもプライバシーの不具合なので、画面より先に出す。V12 は D5 が決まってから
2. §4（イベント）と P15・P16、§6.4（画面の受け方）
3. §6.2 の部品の Space 共通化、§6.1〜§6.3（画面の出し分け・タイムライン・購読）、§6.5（memory への受け渡し。memory の詳細画面と一緒に）、§6.6（compose）
4. §5（presence の送信）。実機での確認（§7.3）を含む
5. §8 の仕様書への反映

## 11. 返信のスレッドと写真の端末保存（追加）

本書の §6.2・§6.5 のうち、返信と写真に関わる部分はここで置き換える。仕様の本体は backend の 10・08・13 と android README にある。

### 11.1 返信の見せ方

| 項目 | 内容 | 仕様 |
| --- | --- | --- |
| タイムライン | `GET /instances/{id}/posts?roots=1` で返信ではない投稿だけを出す。party のタイムラインと写真グリッド、group のタイムラインで使う | 10 §4.2 |
| コメント数 | 各投稿の `replyCount` を使う。返信の `post.created` を受けたら、タイムラインに足さず（取り直しもせず）、`reply.root` の投稿のコメント数を +1 する | 10 §4.2、§7.3 |
| 返信シート | 背景は開いた画面（party なら `PartyScreen`、group なら `GroupScreen`）。シートはデータ取得から切り離した部品で、中身は `GET /instances/{id}/thread` から取る。party を group に詰め替える処理は持たない | 10 §4.2.1 |
| ルート | `/party/replies/?id=&post=`、`/group/replies/?id=&post=`。閉じると `/party/`、`/group/?id=` に戻る | ui-v2 §4.4 |
| 指定された投稿 | タイムラインに無くてもスレッド API で直接取って開く。404 なら「この投稿は見つかりません」 | 10 §4.2.1 |
| 入力欄 | `GET /instances` の `canWrite` が false（仮 party、archived、離脱済み）なら出さない。返信のいいねは出さない（API なし） | 13 §2 |
| 送信 | `POST /posts` の `reply: { root, parent }`。送信中は末尾に仮の行を出し、失敗したら消して本文を入力欄に戻す。成功したら背景の元の投稿のコメント数を +1 | 10 §4.1 |
| リアルタイム | シートを開いている間、`reply.root` が開いているスレッドの root の `post.created` を末尾に足す | 10 §7.3 |

### 11.2 写真の端末保存

- 写真の自動共有（カメラの写真の監視と自動アップロード、写真の読み取り権限）は廃止した。party への写真は compose から手動で共有する
- 設定（端末ごと、既定オフ）でオンにしたときだけ、他のメンバーの写真を `Pictures/CHAIN/{party 名}` に自動保存する。終了の Notice の「写真を保存」からは設定に関係なく保存できる。仕様は backend 08 §5、native は android README §3.1
- 実装は `web/src/device/photos/`（`settings.ts`、`download.ts`）と `ChainPhotos` プラグイン（`savePhoto` のみ）。前面にいる間だけ保存する
- §6.5 の「archived は書けない」は、写真の共有だけ例外がある。終了から 24 時間以内、終了時に未離脱、`takenAt` が party の期間内の写真は archived でも追加できる（10 §5）。memory 一覧の `confirmedAt` / `endedAt`（13 §2）が、どの party に送るかの判定に使われる
- 同じ写真の再送は `200` で既存の record が返る（10 §4.4）
