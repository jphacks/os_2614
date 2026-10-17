#!/usr/bin/env python3
"""specification/backend/18-code-map.md を作る（backend/ で `make codemap`）。

- 機能とコードのパス: 各層（controller / service / model）の機能フォルダーを、そのまま機能として並べる。
  層の直下に残っている 1 ファイルの機能は、ファイル名を機能名にする。
  機能名の表示と仕様の対応は下の FEATURES に書く。FEATURES にない機能は「名前の表にない機能」に出る
- コントローラーとパス: main.go の controller.Routes と、各コントローラーの Register から読む
"""
import glob
import os
import re
import sys

ROOT = os.path.normpath(os.path.join(os.path.dirname(__file__), '..'))
SPEC = os.path.normpath(os.path.join(ROOT, '..', 'specification', 'backend'))
LAYERS = ['controller', 'service', 'model']

# 機能のキー（フォルダー名、または直下のファイル名） → (表示名, 仕様)
FEATURES = {
    'core': ('service の共通部分（Deps・定数・共通の型）', '01 §3'),
    'helpers': ('層の共通部分', '01 §3'),
    'routes': ('ルートの登録', '01 §3'),
    'auth': ('認証・アプリセッション・アカウント削除', '03 §2・§3、13 §5'),
    'oauth': ('ATProto OAuth のセッション', '03 §2.2'),
    'session': ('アプリセッション', '03 §3'),
    'me': ('自分（設定・同意・ステータス・プロフィール編集）', '13 §1、02 §8'),
    'settings': ('ユーザー設定', '13 §1'),
    'user': ('ユーザーとプロフィールの取得', '13 §1.2'),
    'user_handle': ('handle の更新', '03 §5'),
    'profile': ('プロフィール', '13 §1'),
    'proximity': ('近接判定・beacon token・位置', '05'),
    'beacon': ('beacon token', '05 §2'),
    'location': ('位置', '05 §4'),
    'encounter': ('すれ違い・統計', '06、13 §3'),
    'stats': ('統計', '13 §3'),
    'block': ('ブロック', '13 §4'),
    'relationship': ('関係の判定（ブロック・フレンドなど）', '13 §1.2・§4'),
    'friend': ('フレンド・位置共有', '07'),
    'party': ('Party・memory', '08、13 §2'),
    'group': ('グループ（作成・参加・招待・投票・入場通知・グラフ）', '09 §1〜§7'),
    'schedule': ('グループの時間帯 / 予定（自動グループ）', '09 §3、09 §8.2'),
    'natural': ('自動グループ', '09 §8'),
    'access': ('アクセス判定', '10 §5'),
    'content': ('instance・Pin・メディア・フィード・公開投稿・Jetstream・WebSocket', '10、17'),
    'space': ('Space・record・メディア・memory', '04 §2.4、10'),
    'moderation': ('moderation', '11'),
    'modq': ('moderation の再判定キュー', '11 §3'),
    'notify': ('通知（積む・送る・端末・ダイジェスト）', '12'),
    'notification': ('通知', '12'),
    'device': ('端末の登録', '12 §2'),
    'public_post': ('公開投稿', '17 §2'),
    'jetstream': ('Jetstream のカーソル', '03 §5'),
    'maintenance': ('アカウント削除と掃除', '13 §5、04 §4'),
    'janitor': ('掃除', '04 §4'),
    'audit': ('監査ログ', '02 §7'),
    'lock': ('排他（lock:）', '04 §3'),
    'health': ('ヘルスチェック', '—'),
    'ws': ('WebSocket', '10 §7'),
}


def go_files(path):
    return sorted(f for f in glob.glob(os.path.join(path, '*.go'))
                  if not f.endswith('_test.go') and os.path.basename(f) != 'doc.go')


def rel(p):
    return os.path.relpath(p, ROOT)


def feature_table():
    keys = {}
    for layer in LAYERS:
        base = os.path.join(ROOT, 'internal', layer)
        for d in sorted(glob.glob(os.path.join(base, '*', ''))):
            name = os.path.basename(os.path.dirname(d))
            files = [rel(f) for f in go_files(d)]
            if files:
                keys.setdefault(name, {}).setdefault(layer, []).extend(files)
        for f in go_files(base):
            name = os.path.basename(f)[:-3]
            keys.setdefault(name, {}).setdefault(layer, []).append(rel(f))
    rows, unknown = [], []
    order = [k for k in FEATURES if k in keys] + sorted(k for k in keys if k not in FEATURES)
    for k in order:
        label, spec = FEATURES.get(k, (k, '—'))
        if k not in FEATURES:
            unknown.append(k)
        cells = []
        for layer in LAYERS:
            v = keys[k].get(layer, [])
            cells.append('<br>'.join(f'`{p}`' for p in v) if v else '—')
        rows.append(f'| {label}（`{k}`） | {spec} | ' + ' | '.join(cells) + ' |')
    return rows, unknown


def other_dirs():
    out = []
    for name in ['worker', 'external', 'realtime', 'storage']:
        base = os.path.join(ROOT, 'internal', name)
        files = [rel(f) for f in sorted(glob.glob(os.path.join(base, '**', '*.go'), recursive=True))
                 if not f.endswith('_test.go') and os.path.basename(f) != 'doc.go']
        out.append(f'| `internal/{name}/` | ' + '<br>'.join(f'`{p}`' for p in files) + ' |')
    return out


def parse_mount():
    src = open(os.path.join(ROOT, 'main.go')).read()
    body = re.search(r'controller\.Mount\((.*?)\n\t\}\)', src, re.S).group(1)
    groups = {}
    for g in ['Root', 'Public', 'Private']:
        gm = re.search(g + r': \[\]controller\.Registrar\{(.*?)\n\t\t\},', body, re.S)
        for typ in re.findall(r'&\w+\.(\w+)\{', gm.group(1) if gm else ''):
            groups[typ] = g
    return groups


def parse_controllers():
    ctrls = {}
    for f in sorted(glob.glob(os.path.join(ROOT, 'internal', 'controller', '**', '*.go'), recursive=True)):
        if f.endswith('_test.go'):
            continue
        src = open(f).read()
        for m in re.finditer(r'func \(h \*(\w+)\) Register\(r fiber\.Router\) \{(.*?)\n\}', src, re.S):
            typ, body = m.group(1), m.group(2)
            routes = re.findall(r'r\.(Get|Post|Put|Patch|Delete)\("([^"]+)",\s*h\.(\w+)', body)
            # websocket.New(h.handle, ...) のように、ハンドラを包んで渡す登録
            routes += re.findall(r'r\.(Get|Post|Put|Patch|Delete)\("([^"]+)",\s*\w+\.New\(h\.(\w+)', body)
            ctrls[typ] = (rel(f), routes)
    return ctrls


def parse_ratelimit():
    src = open(os.path.join(ROOT, 'internal', 'httpx', 'ratelimit.go')).read()
    table = {(m.upper(), p): b.replace('Bucket', '').lower()
             for m, p, b in re.findall(r'\{fiber\.Method(\w+), "([^"]+)", (Bucket\w+)\}', src)}
    auth = {(m.upper(), p) for m, p in re.findall(r'\{fiber\.Method(\w+), "(/[^"]*auth[^"]*)"\}', src)}
    return table, auth, 'instanceMediaPath' in src


def norm(p):
    return re.sub(r'\{[^}]+\}', '{}', re.sub(r':\w+', '{}', p))


def parse_spec_refs():
    refs = {}
    for line in open(os.path.join(SPEC, '14-api.md')):
        cells = [c.strip() for c in line.strip().strip('|').split('|')]
        if len(cells) < 5 or not cells[1].startswith('`'):
            continue
        methods = [x.strip() for x in cells[0].split('/')]
        paths = [p.strip('`').split('?')[0] for p in re.split(r'`、`', cells[1])]
        for p in paths:
            # `/a/b/accept`、`/decline` のような省略表記は、先頭のパスの最後の要素を置き換える
            if not p.startswith('/') or (p.count('/') == 1 and paths[0].count('/') > 1):
                p = paths[0].rsplit('/', 1)[0] + '/' + p.lstrip('/')
            for mth in methods:
                refs[(mth, norm(p))] = cells[-1]
    return refs


def main():
    groups = parse_mount()
    ctrls = parse_controllers()
    rl, auth_ip, special_media = parse_ratelimit()
    refs = parse_spec_refs()
    feat_rows, unknown = feature_table()

    out = []
    w = out.append
    w('# 18 コードの地図')
    w('')
    w('機能とコードのパス、コントローラーとそのパスの一覧。')
    w('**このファイルは `backend/scripts/codemap.py` が生成する。手で直さず、コードを変えたら作り直す**（`backend/` で `make codemap`）。')
    w('フォルダーの分け方の規則は 01 §3 と、各層の `doc.go`。')
    w('')
    w('## 1. 機能とコードのパス')
    w('')
    w('各層の機能フォルダーと、層の直下に残っている 1 ファイルの機能。パスは `backend/` からの相対。テスト（`*_test.go`）と `doc.go` は省く。')
    w('')
    w('| 機能 | 仕様 | controller | service | model |')
    w('| --- | --- | --- | --- | --- |')
    out.extend(feat_rows)
    w('')
    if unknown:
        w('名前の表にない機能（`codemap.py` の `FEATURES` に足す）: ' + '、'.join(f'`{k}`' for k in unknown))
        w('')
    w('層に属さないもの:')
    w('')
    w('| フォルダー | ファイル |')
    w('| --- | --- |')
    out.extend(other_dirs())
    w('')
    w('共通の部品: `internal/httpx/`（エラー・認証・レート制限・Bind）、`internal/auth/`（JWT・暗号化）、`internal/db/`、`internal/kv/`、`internal/config/`、`internal/clock/`、`internal/idgen/`、`internal/utils/`、`internal/testutil/`、`cmd/dbreset/`、`cmd/keygen/`')
    w('')
    w('## 2. コントローラー一覧')
    w('')
    w('| コントローラー | ファイル | 登録先 | パスの数 |')
    w('| --- | --- | --- | --- |')
    where_of = {'Root': 'ルート（`/api/v1` の外、認証なし）', 'Public': '`/api/v1`（認証なし）', 'Private': '`/api/v1`（認証あり）'}
    for t, g in groups.items():
        f, routes = ctrls.get(t, ('?', []))
        w(f'| `{t}` | `{f}` | {where_of[g]} | {len(routes)} |')
    missing = [t for t in ctrls if t not in groups]
    if missing:
        w('')
        w('`main.go` で登録されていないコントローラー: ' + '、'.join(f'`{t}`' for t in missing))
    w('')
    w('## 3. パスの一覧')
    w('')
    w('- 認証: 「要」は `Authorization: Bearer` のアクセス JWT が要る（03 §3）。WebSocket は接続後の `auth` メッセージで認証する（10 §7.1）')
    w('- レート制限: 02 §5 のバケット。認証ありのパスは専用のバケットに加えて `default` も数える')
    w('')
    w('| メソッド | パス | コントローラー | ハンドラ | 認証 | レート制限 | 仕様 |')
    w('| --- | --- | --- | --- | --- | --- | --- |')
    for t, g in groups.items():
        _, routes = ctrls.get(t, ('?', []))
        for meth, path, handler in routes:
            full = path if g == 'Root' else '/api/v1' + path
            m = meth.upper()
            if t == 'WSController':
                authn, bucket = 'auth メッセージ', 'なし'
            elif g == 'Private':
                authn = '要'
                b = rl.get((m, full))
                if b is None and special_media and m == 'POST' and re.fullmatch(r'/api/v1/instances/:\w+/media', full):
                    b = 'media'
                bucket = f'`{b}` ＋ `default`' if b else '`default`'
            else:
                authn = '不要'
                bucket = '`auth`（IP 単位）' if (m, full) in auth_ip else 'なし'
            ref = refs.get((m, norm(full)), '—')
            shown = re.sub(r':(\w+)', r'{\1}', full)
            w(f'| {m} | `{shown}` | `{t}` | `{handler}` | {authn} | {bucket} | {ref} |')
    sys.stdout.write('\n'.join(out) + '\n')


if __name__ == '__main__':
    main()
