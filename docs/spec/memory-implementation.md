# Memory 実装メモ（13 §2 の補足）

`specification/backend/13-profile-memory-stats.md` §2（以下 13）と `docs/spec/ui-v2.md` §4.7・§4.8 に対して、いまのコードがどこまで来ているか、何が決まっていないかをまとめる。
party 側の話は `docs/spec/party-implementation.md`（以下 party 実装）にある。

## 1. memory とは

- 終わった party と、archive されたグループを、後から見返す場所（draft「archive された party / グループはここで見られる」）
- 想定は「**ふだんの party 画面を、そのまま読み取り専用にした状態**」。投稿・返信・いいねなどはできず、見るだけにする
- 例外として、**自分の投稿と写真は後から消せる**（13「自分の投稿や写真を消したい場合は、個別に削除する」）

## 2. いまの実装

### 2.1 backend：一覧と非表示は揃っている

| API | 実装 | 中身 |
| --- | --- | --- |
| `GET /memories?cursor=` | `controller/party/memory.go`、`PartyService.Memories`、`model/space/memory.go` `ListMemories` | archived の Space のうち、自分が `hidden_from_memory = 0` のもの。終わりの時刻の新しい順 |
| `DELETE /memories/{spaceId}` | `PartyService.HideMemory` | 自分の memory から隠すだけ（`hidden_from_memory = 1`）。他のメンバーには残る。archived でない、またはメンバーでなければ 404 |
| 中身の閲覧 | instance の API（`GET /instances/{id}/posts` など、10 §4.2） | archived 用の API は無く、ふだんの API をそのまま使う |

一覧の各項目の作り方：

| 項目 | party | グループ |
| --- | --- | --- |
| `from` | `tentative_at`（仮 party になった時刻） | `created_at` |
| `until` | `ended_at`（無ければ `archived_at`） | `archived_at` |
| `memberCount` | 離脱済みを含む全メンバー | 同じ |
| `mediaCount` | 削除されていない写真の数 | 同じ |
| `cover` | 最初に共有された写真のサムネイル（署名付き URL） | 同じ |

- 13 では「P0 は Party、P1 でグループも」だが、**グループも既に含めている**（`TestMemoriesMixPartiesAndGroups`）
- 破棄された仮 party は行ごと消えるので、memory には出ない（08 §4.2）

### 2.2 backend：読み取り専用はサーバーで成立している

archived の Space は、アクセス判定（`service/access/access.go` `checkArchived`）で read だけを許し、write は `410 gone` にする（10 §5）。

| 操作 | archived の party | 判定 |
| --- | --- | --- |
| 投稿・写真・Pin の閲覧 | できる（離脱済みのメンバーも含む） | read |
| WebSocket の購読 | できる（何も届かない） | read |
| 新しい投稿 | 410 | write |
| 返信 | 410（返信も `POST /posts` で送るため） | write |
| 写真の共有 | 410 | write |
| Pin を付ける・外す | 410 | write |
| 名前の変更・lock・退出 | 410（party 側の判定） | — |
| **自分の投稿の削除** | **できる**（13 のとおり） | read |
| いいね | API が無い（リアクションは P2） | — |

この表をまとめて確かめるテストは、まだ無い（party 実装 §7.1 の P20 として足す）。

### 2.3 web

| 画面 | 状態 |
| --- | --- |
| `/memory/`（`app/memory/page.tsx`、`MemoryScreen`、`MemoryCard`） | 一覧・もっと見る・メモリーから隠す・読み込み中・エラー・空状態まである。e2e は `web/e2e/memory.spec.ts` |
| カードを押したとき | **何も起きない**。リンクも遷移も無い |
| 終わった party の中身を見る画面 | **無い** |
| プロフィールの「メモリー」横スクロール（`ui-v2.md` §4.7、`MemoryThumbCard`） | 部品はあるが、`app/profile/page.tsx` が `memories={[]}` を渡していて、**常に空** |
| `GET /instances` の一覧（チャット一覧） | 画面が無い。13 の「archived を開くと memory の画面に遷移する」は成り立っていない |

## 3. 問題点

| # | 内容 | 影響 |
| --- | --- | --- |
| M1 | 終わった party の中身を見る画面が無い。仕様にも定義が無い（13 は API まで、`ui-v2.md` §4.8 は一覧のカードまで） | backend は読めるのに、写真も投稿も見返せない |
| M2 | プロフィールのメモリー欄に `GET /memories` がつながっていない | プロフィールではいつも「メモリー無し」になる |
| M3 | 古い前提のコメント・表示が残っている | `memory/page.tsx` と `MemoryScreen` に「`DELETE /memories` は backend 未実装」とある。実装済みなので、本当の失敗が「準備中」と出るおそれがある。e2e の「準備中」のテストも同じ前提 |
| M4 | 画面の補足が「終わったパーティは、ここに残ります」 | グループも出るので、文言が合わない |
| M5 | 返信シート（`ReplySheet`）は `onSubmit` を渡さないと入力欄を押せなくするが、入力欄は残り、文言が「返信は準備中です」になる | memory で使うと「準備中」と出てしまう。`onSubmit` を渡すと、送ってから 410 で失敗する |
| M6 | 明示的に退出した人も、archive を読める（`LeavePartyMember` は `can_read = 1` のまま） | 退出した後の投稿や写真まで見える。08 §4.2 の「archive を読めるように」はこの動きを意図しているが、明示的な退出でも同じでよいかは決まっていない |

## 4. 詳細画面の案（M1）

### 4.1 ルートと入口

- ルートは `/memory/detail/?id={spaceId}`（static export なので query で渡す。`/group/?id=` と同じ形）
- 入口は `/memory/` のカードと、プロフィールの横スクロールのカード

### 4.2 取得

| データ | API |
| --- | --- |
| ヘッダー（名前・期間・人数・表紙） | `GET /memories` の 1 件。一覧から来たときは一覧の値をそのまま使う。直接開かれたときに 1 件だけ取る API は無いので、`GET /memories/{spaceId}` を足すか、一覧から探す |
| メンバー | party は `GET /party/current` が使えない（終わっているため）。グループは `GET /groups/{id}/members`。party 用には、メンバーを返す API を足す必要がある |
| 投稿・写真 | `GET /instances/{id}/posts`（ページ送り）。写真グリッドは読み込んだ投稿の `media` から作る |
| 返信 | 同じ一覧の `reply` で親子をつなぐ |

### 4.3 表示

ふだんの party 画面（party 実装 §6）と同じ並びにし、操作だけを外す。

| 要素 | ふだんの party 画面 | memory の詳細 |
| --- | --- | --- |
| ヒーロー | 経過時間（1 秒ごとに更新）と進捗 | 「{M/D}–{M/D}・{期間}・{n}人」で止める |
| 固定・名前の変更・退出 | 出す | 出さない |
| メンバー | 在席の点を付ける | 在席の点は出さない |
| 写真グリッド | 出す | 出す |
| タイムライン | 出す | 出す |
| 投稿する（Fab） | 出す | 出さない |
| 返信 | シートで読み書き | シートで読むだけ。入力欄そのものを出さない（M5） |
| 自分の投稿の削除 | 出す | 出す（13） |
| 右上のメニュー | — | 「メモリーから隠す」 |

- リアルタイムの購読はしない（archived には何も届かない）
- `410 gone` を受けたら（開いている間に party が終わった場合など）、ふだんの party 画面からこの画面に移す

### 4.4 部品の流用

party 画面と memory の詳細で同じタイムラインを使うため、group 画面のタイムラインを Space 共通の部品に切り出す（`readOnly` を持たせる）。返信シート（`GroupReplyScreen` / `ReplySheet`）も同様に、ヘッダーを差し替えられるようにし、`readOnly` のときは入力欄ごと隠す（今の `onSubmit` 無しは「準備中」の表示なので、それとは分ける）。

## 5. 決めてほしいこと

| # | 論点 | 仮置き |
| --- | --- | --- |
| D1 | 詳細画面のルート | `/memory/detail/?id=` |
| D2 | 1 件だけ取る API と、終わった party のメンバー一覧の API を足すか | 足す（`GET /memories/{spaceId}` にメンバーも含める） |
| D3 | 明示的に退出した人に archive を読ませるか（M6） | いまのまま読ませる（08 §4.2 の意図） |
| D4 | memory の一覧にグループを含めるか | 含める（実装済み）。M4 の文言を「終わったパーティやグループは、ここに残ります」にする |

## 6. 実装の順番

1. M3（古いコメント・表示・e2e）と M4（文言）。すぐ直せる
2. M2（プロフィールに `GET /memories` をつなぐ）
3. D2 の API と、§4 の詳細画面。party 実装 §6.2 のタイムラインの共通化と一緒に進める

## 7. 確かめていないこと

- メンバー全員がアカウントを削除したときに、archived の party が消えるか（13「全員削除したら Space ごと消す」）。`Janitor.reattachCreatedSpaces` は作成者が消えたときだけを見ている。party の作成者が誰になっているかは見ていない
