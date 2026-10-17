#!/usr/bin/env python3
"""specification/backend/18-code-map.md を作る。

  python3 backend/scripts/codemap.py > specification/backend/18-code-map.md

- コントローラーとパスは main.go の controller.Routes と各 controller の Register から読む
- 機能とコードのパスは、下の FEATURES の割り当てに従う。どの機能にも入っていないファイルは
  「未分類」に出るので、ファイルを足したら FEATURES も直す
"""
import glob
import os
import re
import sys

ROOT = os.path.normpath(os.path.join(os.path.dirname(__file__), '..'))
SPEC = os.path.normpath(os.path.join(ROOT, '..', 'specification', 'backend'))
LAYERS = ['controller', 'service', 'model', 'worker', 'external', 'realtime', 'storage']

# 機能 → (仕様, ファイル名の指定の一覧)。指定は stem（拡張子なし。全レイヤーで探す）、
# stem@レイヤー（例 schedule@service）、stem@external のパッケージ（例 client@embed）のどれか。
FEATURES = [
    ('認証・アプリセッション', '03 §2・§3', ['auth', 'session', 'oauth', 'store', 'account']),
    ('自分（設定・同意）', '13 §1、02 §8', ['me', 'settings']),
    ('プロフィール', '13 §1', ['user', 'profile', 'profile_image', 'user_handle']),
    ('近接判定の共通部分', '05', ['proximity', 'beacon', 'beacon_payload', 'geohash', 'params']),
    ('位置', '05 §4', ['location']),
    ('すれ違い', '06', ['encounter', 'encounter_summary', 'encounter_revealer', 'encounter_feed']),
    ('ブロック', '13 §4', ['block', 'relationship']),
    ('フレンド', '07 §1〜§3', ['friend']),
    ('位置共有', '07 §4', ['location_share']),
    ('Party', '08', ['party', 'party_engine', 'memory']),
    ('グループ', '09 §1〜§5・§9', ['group', 'group_circle', 'schedule@service']),
    ('closed グループの招待', '09 §6', ['group_invite']),
    ('入場通知・グラフ', '09 §6.1・§7', ['group_entry', 'group_graph']),
    ('自動グループ', '09 §8', ['natural_groups', 'natural_cluster', 'schedule_entries', 'schedule_match',
                         'schedule@controller', 'schedule@model', 'embedder', 'embed_backfill', 'client@embed']),
    ('投票', '09 §8.6', ['group_vote', 'group_votes']),
    ('instance（Space の投稿）', '10 §1〜§5', ['instance', 'record', 'space', 'access']),
    ('Pin', '10 §6', ['pin']),
    ('メディア', '10 §4.4', ['media', 'media_serve', 'sign']),
    ('WebSocket', '10 §7', ['ws', 'realtime', 'hub']),
    ('公開投稿・Jetstream', '17 §2、03 §4・§5', ['public_post', 'writer', 'jetstream', 'firstload']),
    ('フィード', '17 §3', ['feed']),
    ('moderation', '11', ['moderation', 'moderation_jev', 'moderation_prompts', 'moderation_queue',
                       'moderation_requeue', 'modq', 'client@nanogpt']),
    ('通知', '12', ['notification', 'notify_sender', 'digest', 'device', 'client@fcm', 'credentials', 'wire']),
    ('統計', '13 §3', ['stats']),
    ('掃除・排他', '04 §4', ['janitor', 'lock', 'audit']),
    ('ヘルスチェック', '—', ['health']),
]


def go_files(layer):
    pattern = os.path.join(ROOT, 'internal', layer, '**', '*.go') if layer == 'external' \
        else os.path.join(ROOT, 'internal', layer, '*.go')
    out = []
    for f in sorted(glob.glob(pattern, recursive=True)):
        name = os.path.basename(f)
        if name.endswith('_test.go') or name == 'doc.go':
            continue
        out.append(os.path.relpath(f, ROOT))
    return out


def keys_of(path):
    """ファイルに当てはまる指定。stem、stem@レイヤー、external なら stem@パッケージ。"""
    stem = os.path.basename(path)[:-3]
    parts = path.split('/')
    layer = parts[1]
    keys = {stem, f'{stem}@{layer}'}
    if layer == 'external':
        keys.add(f'{stem}@{parts[2]}')
    return stem, keys


def feature_table():
    files = [f for layer in LAYERS for f in go_files(layer) if f != 'internal/controller/routes.go']
    # どこかで stem@… と限定して指定されている stem は、限定した指定でしか当てない
    qualified = {k.split('@')[0] for _, _, ks in FEATURES for k in ks if '@' in k}
    assigned = set()
    rows = []
    for name, spec, specs in FEATURES:
        hits = []
        for f in files:
            if f in assigned:
                continue
            stem, keys = keys_of(f)
            wanted = set(specs) if stem not in qualified else {k for k in specs if '@' in k}
            if keys & wanted:
                hits.append(f)
        assigned.update(hits)
        by_layer = {}
        for f in hits:
            by_layer.setdefault(f.split('/')[1], []).append(f)
        cells = ['<br>'.join(f'`{p}`' for p in by_layer.get(layer, [])) or '—' for layer in LAYERS]
        rows.append(f'| {name} | {spec} | ' + ' | '.join(cells) + ' |')
    rest = [f for f in files if f not in assigned]
    return rows, rest


def parse_mount():
    src = open(os.path.join(ROOT, 'main.go')).read()
    m = re.search(r'controller\.Mount\((.*?)\n\t\}\)', src, re.S)
    body = m.group(1)
    groups = {}
    for g in ['Root', 'Public', 'Private']:
        gm = re.search(g + r': \[\]controller\.Registrar\{(.*?)\n\t\t\},', body, re.S)
        for t in re.findall(r'&controller\.(\w+)\{', gm.group(1) if gm else ''):
            groups[t] = g
    return groups


def parse_controllers():
    ctrls = {}
    for f in go_files('controller'):
        src = open(os.path.join(ROOT, f)).read()
        for m in re.finditer(r'func \(h \*(\w+)\) Register\(r fiber\.Router\) \{(.*?)\n\}', src, re.S):
            typ, body = m.group(1), m.group(2)
            routes = re.findall(r'r\.(Get|Post|Put|Patch|Delete)\("([^"]+)",\s*h\.(\w+)', body)
            # websocket.New(h.handle, ...) のように、ハンドラを包んで渡す登録
            routes += re.findall(r'r\.(Get|Post|Put|Patch|Delete)\("([^"]+)",\s*\w+\.New\(h\.(\w+)', body)
            ctrls[typ] = (f, routes)
    return ctrls


def parse_ratelimit():
    src = open(os.path.join(ROOT, 'internal', 'httpx', 'ratelimit.go')).read()
    table = {}
    for meth, path, bucket in re.findall(r'\{fiber\.Method(\w+), "([^"]+)", (Bucket\w+)\}', src):
        table[(meth.upper(), path)] = bucket.replace('Bucket', '').lower()
    auth = {(meth.upper(), path) for meth, path in re.findall(r'\{fiber\.Method(\w+), "(/[^"]*auth[^"]*)"\}', src)}
    special_media = 'instanceMediaPath' in src
    return table, auth, special_media


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


def norm(p):
    p = re.sub(r':\w+', '{}', p)
    return re.sub(r'\{[^}]+\}', '{}', p)


def main():
    groups = parse_mount()
    ctrls = parse_controllers()
    rl, auth_ip, special_media = parse_ratelimit()
    refs = parse_spec_refs()
    feat_rows, rest = feature_table()

    out = []
    w = out.append
    w('# 18 コードの地図')
    w('')
    w('機能とコードのパス、コントローラーとそのパスの一覧。')
    w('**このファイルは `backend/scripts/codemap.py` が生成する。手で直さず、コードを変えたら作り直す**（`backend/` で `make codemap`）。')
    w('')
    w('## 1. 機能とコードのパス')
    w('')
    w('パスは `backend/` からの相対。テスト（`*_test.go`）と `doc.go` は省く。')
    w('')
    w('| 機能 | 仕様 | controller | service | model | worker | external | realtime | storage |')
    w('| --- | --- | --- | --- | --- | --- | --- | --- | --- |')
    out.extend(feat_rows)
    w('')
    if rest:
        w('未分類のファイル（`codemap.py` の `FEATURES` に足す）: ' + '、'.join(f'`{p}`' for p in rest))
    else:
        w('未分類のファイルはない。')
    w('')
    w('共通の部品（機能に属さないもの）: `internal/controller/routes.go`（ルートの登録）、`internal/httpx/`（エラー・認証・レート制限・Bind）、`internal/auth/`（JWT・暗号化）、`internal/db/`、`internal/kv/`、`internal/config/`、`internal/clock/`、`internal/idgen/`、`internal/utils/`、`internal/testutil/`、`cmd/dbreset/`、`cmd/keygen/`')
    w('')
    w('## 2. コントローラー一覧')
    w('')
    w('| コントローラー | ファイル | 登録先 | パスの数 |')
    w('| --- | --- | --- | --- |')
    order = list(groups.keys())
    for t in order:
        f, routes = ctrls.get(t, ('?', []))
        where = {'Root': 'ルート（`/api/v1` の外、認証なし）', 'Public': '`/api/v1`（認証なし）', 'Private': '`/api/v1`（認証あり）'}[groups[t]]
        w(f'| `{t}` | `{f}` | {where} | {len(routes)} |')
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
    for t in order:
        f, routes = ctrls.get(t, ('?', []))
        g = groups[t]
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
            w(f'| {m} | `{re.sub(r":(\w+)", r"{\1}", full)}` | `{t}` | `{handler}` | {authn} | {bucket} | {ref} |')
    sys.stdout.write('\n'.join(out) + '\n')


if __name__ == '__main__':
    main()
