#!/usr/bin/env python3
"""デモ動画用の仕込み。python3 seed.py <phase>
phase: base | encounters | party | feed | clean
"""
import hashlib, io, json, os, sqlite3, subprocess, sys, time, uuid

from PIL import Image, ImageDraw, ImageFont

ROOT = "/Users/ringo/Documents/docs/projects/os_2614/backend"
DB = f"{ROOT}/data/app.db"
MEDIA = f"{ROOT}/data/media"
REDIS = ["redis-cli", "-p", "6380"]
DEMO = "did:plc:gfxb53wub7nukkiozpx62van"   # chain-demo-1018.bsky.social
APPLE = "did:plc:oo764tcvvcuo6kuk55772okg"  # abc.osaka (applemango)
LAT, LON = 34.6517397, 135.5911797          # 端末の位置

now = int(time.time() * 1000)
MIN = 60_000

FAKES = [
    ("haruka", "はるか", "写真とカフェめぐり📷"),
    ("souta", "そうた", "JPHACKS 出場中！バックエンド担当"),
    ("mio", "みお", "ボドゲ部 / 情報学部 2 年"),
    ("kento", "けんと", "バスケと音楽"),
    ("yui", "ゆい", "図書館にだいたいいます"),
    ("riku", "りく", "フロントエンドが好き"),
]
def fdid(k): return f"did:web:{k}.demo.chain.i32.jp"

# (key, name, description, dlat, dlon, radius, emoji, bg, members)
GROUPS = [
    ("campus", "キャンパス", "大学のキャンパス全体", -0.0010, -0.0020, 700, "🏫", "#DCEBFF",
     ["haruka", "souta", "mio", "kento", "yui", "riku"]),
    ("hack", "JPHACKS 会場", "ハッカソンの会場", 0.0, 0.0, 60, "💻", "#E6F4EA",
     ["souta", "riku", "mio", "haruka"]),
    ("boardgame", "ボドゲ部", "放課後にボードゲーム", 0.0002, 0.0001, 15, "🎲", "#FDE7EF",
     ["mio", "riku", "souta"]),
    ("library", "図書館", "静かに勉強する人たち", 0.0018, 0.0005, 50, "📚", "#FFF4D6",
     ["yui", "haruka", "mio"]),
    ("cafe", "カフェテリア", "お昼ごはん", -0.0005, 0.0030, 40, "☕", "#F3E9DD",
     ["haruka", "kento", "yui"]),
    ("gym", "体育館", "バスケ・バレー", -0.0030, -0.0010, 80, "🏀", "#FFE7D6",
     ["kento", "riku"]),
]
def gid(i): return f"01a14d00-0000-7000-8000-{i:012d}"

def sh(cmd): subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL)

def icon(emoji, bg, size):
    im = Image.new("RGB", (size, size), bg)
    font = ImageFont.truetype("/System/Library/Fonts/Apple Color Emoji.ttc", 160)
    e = Image.new("RGBA", (200, 200), (0, 0, 0, 0))
    ImageDraw.Draw(e).text((20, 20), emoji, font=font, embedded_color=True)
    e = e.crop(e.getbbox()).resize((int(size * .58),) * 2, Image.LANCZOS)
    im.paste(e, ((size - e.width) // 2, (size - e.height) // 2), e)
    b = io.BytesIO(); im.save(b, "JPEG", quality=92); return b.getvalue()

def base(c):
    for k, name, desc in FAKES:
        d = fdid(k)
        c.execute("INSERT OR REPLACE INTO users (did, handle, pds_url, created_at) VALUES (?,?,?,?)",
                  (d, f"{k}.demo.i32.jp", "https://seed.invalid", now - 30 * 86400_000))
        c.execute("INSERT OR REPLACE INTO profiles (did, display_name, description, updated_at) VALUES (?,?,?,?)",
                  (d, name, desc, now))
        c.execute("""INSERT OR REPLACE INTO user_settings (did, encounter_mode, encounter_delay_sec,
                     content_scan_consent_version, content_scan_consent_at, updated_at) VALUES (?,?,?,?,?,?)""",
                  (d, "immediate", 0, 1, now, now))
    keys = {g[0]: i + 1 for i, g in enumerate(GROUPS)}
    for i, (k, name, desc, dlat, dlon, r, emoji, bg, members) in enumerate(GROUPS, 1):
        sid, owner = gid(i), fdid(members[0])
        parent = gid(keys["campus"]) if k not in ("campus", "boardgame") else (gid(keys["hack"]) if k == "boardgame" else None)
        c.execute("""INSERT OR REPLACE INTO spaces (id, type, authority, ref, name, state, created_by, created_at)
                     VALUES (?,?,?,?,?,?,?,?)""",
                  (sid, "jp.i32.chain.space.group", "did:web:chain.i32.jp",
                   f"at://did:web:chain.i32.jp/space/jp.i32.chain.space.group/{sid}", name, "active", owner, now - 7 * 86400_000))
        mid = f"01a14d00-0000-7000-9000-{i:012d}"
        created = now - 7 * 86400_000
        full, thumb = icon(emoji, bg, 512), icon(emoji, bg, 256)
        ym = time.strftime("%Y/%m", time.gmtime(created / 1000))
        os.makedirs(f"{MEDIA}/{ym}", exist_ok=True)
        open(f"{MEDIA}/{ym}/{mid}.jpg", "wb").write(full)
        open(f"{MEDIA}/{ym}/{mid}_t.jpg", "wb").write(thumb)
        c.execute("""INSERT OR REPLACE INTO media (id, owner_did, space_id, mime, width, height, bytes, sha256, created_at)
                     VALUES (?,?,?,?,?,?,?,?,?)""",
                  (mid, owner, sid, "image/jpeg", 512, 512, len(full), hashlib.sha256(full).hexdigest(), created))
        c.execute("""INSERT OR REPLACE INTO groups (space_id, description, visibility, origin, persistent,
                     center_lat, center_lon, radius_m, parent_space_id, icon_media_id) VALUES (?,?,?,?,?,?,?,?,?,?)""",
                  (sid, desc, "open", "explicit", 1, LAT + dlat, LON + dlon, r, parent, mid))
        for j, m in enumerate(members):
            c.execute("INSERT OR REPLACE INTO space_members (space_id, did, role, joined_at) VALUES (?,?,?,?)",
                      (sid, fdid(m), "owner" if j == 0 else "member", now - 6 * 86400_000))
        sh(REDIS + ["GEOADD", "geo:groups", str(LON + dlon), str(LAT + dlat), sid])

def encounters(c):
    peers = [APPLE] + [fdid(k) for k, _, _ in FAKES[:5]]
    for i, p in enumerate(peers):
        a, b = sorted([DEMO, p])
        t = now - (3 + i * 7) * MIN
        c.execute("""INSERT OR REPLACE INTO encounters (id, user_a, user_b, established_at, first_seen_at, last_seen_at,
                     max_rssi, visibility, reveal_at) VALUES (?,?,?,?,?,?,?,?,?)""",
                  (f"01a14d00-0000-7000-a000-{i:012d}", a, b, t, t, t + 4 * MIN, -60, "revealed", t))

def party(c):
    sid = "01a14d00-0000-7000-b000-000000000001"
    t = now - 6 * MIN
    lt = time.localtime(t / 1000)
    c.execute("""INSERT OR REPLACE INTO spaces (id, type, authority, ref, name, state, created_by, created_at)
                 VALUES (?,?,?,?,?,?,?,?)""",
              (sid, "jp.i32.chain.space.party", "did:web:chain.i32.jp",
               f"at://did:web:chain.i32.jp/space/jp.i32.chain.space.party/{sid}",
               f"{lt.tm_mon}/{lt.tm_mday} {lt.tm_hour}:{lt.tm_min:02d} の party", "active", DEMO, t))
    c.execute("""INSERT OR REPLACE INTO parties (space_id, phase, locked, tentative_at, confirmed_at, last_activity_at)
                 VALUES (?,?,?,?,?,?)""", (sid, "confirmed", 1, t, t + 5 * MIN, now))
    members = [DEMO, APPLE, fdid("haruka"), fdid("souta")]
    for m in members:
        c.execute("INSERT OR REPLACE INTO space_members (space_id, did, role, joined_at) VALUES (?,?,?,?)",
                  (sid, m, "member", t))
        sh(REDIS + ["SET", f"party:of:{m}", sid])
    posts = [(fdid("haruka"), "集合しました！"), (APPLE, "このあとご飯いく？"), (fdid("souta"), "カフェテリア集合で🍛")]
    for i, (a, text) in enumerate(posts):
        rkey = f"3m3demoparty{i}"
        uri = f"at://did:web:chain.i32.jp/space/jp.i32.chain.space.party/{sid}/{a}/jp.i32.chain.instance.post/{rkey}"
        ts = t + (2 + i) * MIN
        c.execute("""INSERT OR REPLACE INTO records (uri, space_id, author_did, collection, rkey, value_json, created_at)
                     VALUES (?,?,?,?,?,?,?)""",
                  (uri, sid, a, "jp.i32.chain.instance.post", rkey,
                   json.dumps({"text": text, "format": "plain",
                               "createdAt": time.strftime("%Y-%m-%dT%H:%M:%S.000Z", time.gmtime(ts / 1000))},
                              ensure_ascii=False), ts))

def feed(c):
    posts = [
        ("haruka", "キャンパスの銀杏がきれい🍂", 4),
        ("souta", "JPHACKS 、デモ動画の撮影中🎬", 11),
        ("mio", "ボドゲ部、今日は 17 時からです🎲", 25),
        ("kento", "体育館あいてたのでバスケしてきた", 48),
        ("yui", "図書館の 3 階、窓ぎわが一番集中できる", 75),
    ]
    for i, (k, text, ago) in enumerate(posts):
        d, ts = fdid(k), now - ago * MIN
        c.execute("""INSERT OR REPLACE INTO public_posts (uri, cid, author_did, text, langs, created_at, indexed_at,
                     mod_label, mod_state, audience) VALUES (?,?,?,?,?,?,?,?,?,?)""",
                  (f"at://{d}/jp.i32.chain.feed.post/3m3demofeed{i}", f"bafyreidemo{i:04d}", d, text,
                   '["ja"]', ts, ts, "ok", "done", "public"))

def clean(c):
    sids = [gid(i) for i in range(1, len(GROUPS) + 1)] + ["01a14d00-0000-7000-b000-000000000001"]
    for s in sids:
        c.execute("DELETE FROM spaces WHERE id = ?", (s,))
    for k, _, _ in FAKES:
        c.execute("DELETE FROM users WHERE did = ?", (fdid(k),))
    sh(REDIS + ["ZREM", "geo:groups", *sids[:-1]])
    for m in [DEMO, APPLE] + [fdid(k) for k, _, _ in FAKES]:
        sh(REDIS + ["DEL", f"party:of:{m}"])

if __name__ == "__main__":
    c = sqlite3.connect(DB, timeout=10)
    c.execute("PRAGMA foreign_keys = ON")
    globals()[sys.argv[1]](c)
    c.commit()
    print("ok", sys.argv[1])
