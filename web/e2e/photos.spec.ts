import { expect, test } from "@playwright/test";
import {
  isPhotoInParty,
  retryDelay,
  selectPartyForPhoto,
  PHOTO_RETRY_MAX_MS,
  PHOTO_RETRY_MIN_MS,
} from "../src/device/photos/autoshare";
import { shouldSavePhoto } from "../src/device/photos/download";
import { ChainPhotosWeb } from "../src/device/photos/web";
import { memoryItemsOrEmpty } from "../src/components/layout/PhotoShareDriver";

const API_BASE = "https://chain.i32.jp";
const SESSION_KEY = "chain.auth.session.v1";
const PHOTO_SETTINGS_KEY = "chain.party.photos.settings.v1";
const PHOTO_TEST_PREPARED_KEY = "chain.photo.test.prepared";
const PHOTO_TEST_CALLS_KEY = "chain.photo.test.calls";
const CORS_HEADERS = {
  "access-control-allow-origin": "*",
  "access-control-allow-methods": "GET,POST,PATCH,PUT,DELETE,OPTIONS",
  "access-control-allow-headers": "authorization,content-type",
};

const ME = {
  did: "did:plc:testuser",
  handle: "test.bsky.social",
  displayName: "テストユーザー",
  settings: {
    encounterMode: "delayed",
    encounterDelaySec: 3600,
    friendBeaconShare: true,
    partyAuto: false,
    locationPaused: false,
    warnDisplay: "blur",
    notify: {},
  },
};

const PARTY = {
  id: "party-photo-1",
  name: "写真パーティ",
  phase: "confirmed",
  locked: false,
  tentativeAt: "2026-10-17T03:00:00.000Z",
  confirmedAt: "2026-10-17T03:01:00.000Z",
  members: [{ did: "did:plc:testuser", handle: "test", displayName: "テスト", present: true, left: false }],
};

function fakeSession(): string {
  return JSON.stringify({
    did: "did:plc:testuser",
    accessToken: "token",
    refreshToken: "refresh",
    accessExpiresAtMs: Date.now() + 3_600_000,
  });
}

async function preparePage(page: import("@playwright/test").Page, settings = { autoShare: false, saveToDevice: false }) {
  await page.addInitScript(({ sessionKey, settingsKey, session, settings: next, preparedKey, callsKey }) => {
    if (!sessionStorage.getItem(preparedKey)) {
      localStorage.setItem(sessionKey, session);
      localStorage.setItem(settingsKey, JSON.stringify(next));
      sessionStorage.setItem(preparedKey, "1");
    }
    const rawCalls = sessionStorage.getItem(callsKey);
    (window as unknown as { __chainPhotosMock: unknown }).__chainPhotosMock = {
      ready: false,
      calls: rawCalls ? JSON.parse(rawCalls) : [],
    };
  }, { sessionKey: SESSION_KEY, settingsKey: PHOTO_SETTINGS_KEY, session: fakeSession(), settings, preparedKey: PHOTO_TEST_PREPARED_KEY, callsKey: PHOTO_TEST_CALLS_KEY });
}

async function mockPhotoApi(
  page: import("@playwright/test").Page,
  options: {
    party?: unknown;
    memories?: unknown;
    posts?: unknown;
    onMedia?: (path: string, body: unknown) => void;
  } = {},
) {
  await page.route(`${API_BASE}/**`, async (route) => {
    const request = route.request();
    if (request.method() === "OPTIONS") {
      await route.fulfill({ status: 204, headers: CORS_HEADERS });
      return;
    }
    const url = new URL(request.url());
    const path = url.pathname;
    if (path === "/api/v1/me") {
      await route.fulfill({ status: 200, headers: { ...CORS_HEADERS, "content-type": "application/json" }, body: JSON.stringify(ME) });
      return;
    }
    if (path === "/api/v1/me/areas") {
      await route.fulfill({ status: 200, headers: { ...CORS_HEADERS, "content-type": "application/json" }, body: JSON.stringify({ items: [] }) });
      return;
    }
    if (path === "/api/v1/party/current") {
      await route.fulfill({ status: 200, headers: { ...CORS_HEADERS, "content-type": "application/json" }, body: JSON.stringify(options.party === undefined ? PARTY : options.party) });
      return;
    }
    if (path === "/api/v1/memories") {
      const memory = options.memories === undefined ? { items: [] } : options.memories;
      await route.fulfill({ status: memory === "404" ? 404 : 200, headers: { ...CORS_HEADERS, "content-type": "application/json" }, body: memory === "404" ? "{}" : memory === "broken" ? "{" : JSON.stringify(memory) });
      return;
    }
    if (path === `/api/v1/instances/${PARTY.id}/posts`) {
      await route.fulfill({ status: 200, headers: { ...CORS_HEADERS, "content-type": "application/json" }, body: JSON.stringify(options.posts ?? { items: [] }) });
      return;
    }
    if (path === "/api/v1/media" || path === `/api/v1/instances/${PARTY.id}/media`) {
      options.onMedia?.(path, undefined);
      await route.fulfill({ status: 200, headers: { ...CORS_HEADERS, "content-type": "application/json" }, body: JSON.stringify({ id: "uploaded-media" }) });
      return;
    }
    if (path === "/api/v1/me/settings" || path === "/api/v1/me/location-sharing") {
      await route.fulfill({ status: 200, headers: { ...CORS_HEADERS, "content-type": "application/json" }, body: JSON.stringify(ME.settings) });
      return;
    }
    await route.fulfill({ status: 404, headers: { ...CORS_HEADERS, "content-type": "application/json" }, body: JSON.stringify({ error: { code: "not_found", message: "not found" } }) });
  });
}

async function emitPhoto(page: import("@playwright/test").Page, id = "camera-1") {
  await page.waitForFunction(() => Boolean((window as unknown as { __chainPhotosMock?: { ready?: boolean } }).__chainPhotosMock?.ready));
  await page.evaluate((mediaStoreId) => {
    const mock = (window as unknown as { __chainPhotosMock: { emit: (photo: unknown) => void } }).__chainPhotosMock;
    mock.emit({ mediaStoreId, takenAt: "2026-10-17T03:10:00.000Z", uri: `content://DCIM/Camera/${mediaStoreId}` });
  }, id);
}

async function waitForPhotoMock(page: import("@playwright/test").Page): Promise<void> {
  await page.waitForFunction(() => Boolean((window as unknown as { __chainPhotosMock?: { ready?: boolean } }).__chainPhotosMock?.ready));
}

test.describe("写真共有の判定", () => {
  test("撮影時刻が party の期間内かを判定する", () => {
    const photo = {
      mediaStoreId: "1",
      takenAt: "2026-01-01T10:00:00Z",
      uri: "content://1",
    };
    expect(
      isPhotoInParty(photo, {
        id: "p1",
        confirmedAt: "2026-01-01T09:00:00Z",
        endedAt: "2026-01-01T11:00:00Z",
      }),
    ).toBe(true);
    expect(
      selectPartyForPhoto(photo, [
        { id: "p1", confirmedAt: "2026-01-01T11:00:00Z", endedAt: "2026-01-01T12:00:00Z" },
        { id: "p2", confirmedAt: "2026-01-01T09:00:00Z", endedAt: "2026-01-01T11:00:00Z" },
      ]),
    ).toEqual({
      id: "p2",
      confirmedAt: "2026-01-01T09:00:00Z",
      endedAt: "2026-01-01T11:00:00Z",
    });
  });

  test("送信リトライは 30 秒から 10 分で上限になる", () => {
    expect(retryDelay(0)).toBe(PHOTO_RETRY_MIN_MS);
    expect(retryDelay(20)).toBe(PHOTO_RETRY_MAX_MS);
  });

  test("自分の写真と非表示の写真は端末へ保存しない", () => {
    const base = {
      id: "m1",
      url: "https://example.test/m.jpg",
      thumbUrl: "https://example.test/m-thumb.jpg",
      alt: "photo",
      width: 1,
      height: 1,
    };
    expect(shouldSavePhoto({ ...base, ownerDid: "did:me" }, "did:me")).toBe(false);
    expect(shouldSavePhoto({ ...base, hidden: true }, "did:me")).toBe(false);
    expect(shouldSavePhoto({ ...base, ownerDid: "did:other" }, "did:me")).toBe(true);
  });

  test("取消した写真は再スキャンしても戻らない", async () => {
    const photos = new ChainPhotosWeb();
    await photos.startWatching({ since: "2026-01-01T09:00:00Z" });
    const photo = {
      mediaStoreId: "camera-1",
      takenAt: "2026-01-01T10:00:00Z",
      uri: "content://DCIM/Camera/camera-1",
    };
    photos.emitPhoto(photo);
    expect((await photos.listPending()).items).toHaveLength(1);
    await photos.removePending({ ids: [photo.mediaStoreId] });
    photos.emitPhoto(photo);
    expect((await photos.listPending()).items).toHaveLength(0);
  });

  test("カメラ以外の写真はモックでも監視対象外", async () => {
    const photos = new ChainPhotosWeb();
    await photos.startWatching({ since: "2026-01-01T09:00:00Z" });
    photos.emitPhoto({
      mediaStoreId: "download-1",
      takenAt: "2026-01-01T10:00:00Z",
      uri: "content://Pictures/Download/download-1",
    });
    expect((await photos.listPending()).items).toHaveLength(0);
  });

  test("権限が拒否された状態をモックできる", async () => {
    const photos = new ChainPhotosWeb();
    photos.setPermission({ photos: "denied" });
    expect((await photos.checkPermissions()).photos).toBe("denied");
    expect((await photos.requestPermissions()).photos).toBe("denied");
  });

  test("memories の 404 や空 JSON は他の画面を壊さない", () => {
    expect(memoryItemsOrEmpty({})).toEqual([]);
    expect(memoryItemsOrEmpty(null)).toEqual([]);
    expect(memoryItemsOrEmpty({ items: [] })).toEqual([]);
  });
});

test.describe("写真共有の画面フロー", () => {
  test("設定で権限 granted はオン、denied / limited はオフと案内", async ({ page }) => {
    await preparePage(page);
    await mockPhotoApi(page);
    await page.goto("/settings/");
    const toggle = page.getByRole("switch", { name: "写真を自動共有" });
    await toggle.click();
    await page.getByRole("button", { name: "許可してオン" }).click();
    await expect(toggle).toHaveAttribute("aria-checked", "true");

    await page.goto("/settings/");
    await page.addInitScript(() => {
      (window as unknown as { __chainPhotosMockPermission: unknown }).__chainPhotosMockPermission = { photos: "denied" };
    });
    await page.evaluate((settingsKey) => localStorage.removeItem(settingsKey), PHOTO_SETTINGS_KEY);
    await page.reload();
    await toggle.click();
    await page.getByRole("button", { name: "許可してオン" }).click();
    await expect(toggle).toHaveAttribute("aria-checked", "false");
    await expect(page.locator('[role="alert"]').filter({ hasText: "許可されなかった" })).toBeVisible();

    await page.addInitScript(() => {
      (window as unknown as { __chainPhotosMockPermission: unknown }).__chainPhotosMockPermission = { photos: "limited" };
    });
    await page.reload();
    await page.getByRole("switch", { name: "写真を自動共有" }).click();
    await page.getByRole("button", { name: "許可してオン" }).click();
    await expect(page.getByRole("switch", { name: "写真を自動共有" })).toHaveAttribute("aria-checked", "false");
  });

  test("confirmed party の写真は 60 秒後に共有される", async ({ page }) => {
    await page.clock.install({ time: new Date("2026-10-17T03:10:00Z") });
    await preparePage(page, { autoShare: true, saveToDevice: false });
    const sent: string[] = [];
    await mockPhotoApi(page, { onMedia: (path) => sent.push(path) });
    await page.goto("/party/");
    await emitPhoto(page);
    await expect(page.getByText("1 枚を共有します")).toBeVisible();
    await page.clock.fastForward(60_000);
    await expect.poll(() => sent.filter((path) => path === "/api/v1/media").length).toBe(1);
    await expect.poll(() => sent.filter((path) => path.endsWith("/media")).length).toBe(2);
  });

  test("待機中に取消すると API に送られず再スキャンでも戻らない", async ({ page }) => {
    await page.clock.install({ time: new Date("2026-10-17T03:10:00Z") });
    await preparePage(page, { autoShare: true, saveToDevice: false });
    const sent: string[] = [];
    await mockPhotoApi(page, { onMedia: (path) => sent.push(path) });
    await page.goto("/party/");
    await emitPhoto(page);
    await page.getByRole("button", { name: "取り消す" }).click();
    await emitPhoto(page);
    await page.clock.fastForward(60_000);
    expect(sent).toEqual([]);
    await expect(page.getByText("1 枚を共有します")).toHaveCount(0);
  });

  test("送信済みの同じ写真を再通知しても一度だけ送る", async ({ page }) => {
    await page.clock.install({ time: new Date("2026-10-17T03:10:00Z") });
    await preparePage(page, { autoShare: true, saveToDevice: false });
    const sent: string[] = [];
    await mockPhotoApi(page, { onMedia: (path) => sent.push(path) });
    await page.goto("/party/");
    await emitPhoto(page);
    await page.clock.fastForward(60_000);
    await expect.poll(() => sent.length).toBe(2);
    await emitPhoto(page);
    await page.clock.fastForward(60_000);
    expect(sent).toHaveLength(2);
  });

  test("終了後24時間以内の写真は archived party へ送り、25時間前は送らない", async ({ page }) => {
    await page.clock.install({ time: new Date("2026-10-18T03:10:00Z") });
    await page.addInitScript(() => {
      (window as unknown as { __chainPhotosMockPending: unknown[] }).__chainPhotosMockPending = [
        { mediaStoreId: "old-1", takenAt: "2026-10-17T03:30:00Z", uri: "content://DCIM/Camera/old-1" },
      ];
    });
    await preparePage(page, { autoShare: true, saveToDevice: false });
    const sent: string[] = [];
    await mockPhotoApi(page, {
      party: null,
      memories: {
        items: [{ spaceId: "party-photo-1", type: "party", name: "終了", from: "2026-10-17T03:01:00Z", until: "2026-10-17T04:10:00Z", confirmedAt: "2026-10-17T03:01:00Z", endedAt: "2026-10-17T04:10:00Z" }],
      },
      onMedia: (path) => sent.push(path),
    });
    const memoriesResponse = page.waitForResponse((response) => response.url().includes("/api/v1/memories"));
    await page.goto("/party/");
    await memoriesResponse;
    await page.waitForTimeout(50);
    await page.clock.fastForward(60_000);
    await expect.poll(() => sent.length).toBe(2);
  });

  test("途中で自動共有をオフにすると待ち行列を捨てる", async ({ page }) => {
    await page.clock.install({ time: new Date("2026-10-17T03:10:00Z") });
    await preparePage(page, { autoShare: true, saveToDevice: false });
    const sent: string[] = [];
    await mockPhotoApi(page, { onMedia: (path) => sent.push(path) });
    await page.goto("/party/");
    await emitPhoto(page);
    await page.goto("/settings/");
    await page.getByRole("switch", { name: "写真を自動共有" }).click();
    await page.clock.fastForward(60_000);
    expect(sent).toEqual([]);
  });

  test("終了から25時間を過ぎた party には送らない", async ({ page }) => {
    await page.clock.install({ time: new Date("2026-10-18T05:10:00Z") });
    await page.addInitScript(() => {
      (window as unknown as { __chainPhotosMockPending: unknown[] }).__chainPhotosMockPending = [
        { mediaStoreId: "too-old", takenAt: "2026-10-18T03:30:00Z", uri: "content://DCIM/Camera/too-old" },
      ];
    });
    await preparePage(page, { autoShare: true, saveToDevice: false });
    const sent: string[] = [];
    await mockPhotoApi(page, {
      party: null,
      memories: { items: [{ spaceId: PARTY.id, type: "party", name: "終了", from: "2026-10-17T03:01:00Z", until: "2026-10-17T04:10:00Z", confirmedAt: "2026-10-17T03:01:00Z", endedAt: "2026-10-17T04:10:00Z" }] },
      onMedia: (path) => sent.push(path),
    });
    await page.goto("/party/");
    await waitForPhotoMock(page);
    await page.clock.fastForward(60_000);
    expect(sent).toEqual([]);
  });

  test("ログアウトで設定と待ち行列を消す", async ({ page }) => {
    await preparePage(page, { autoShare: true, saveToDevice: false });
    await mockPhotoApi(page);
    await page.goto("/settings/");
    await waitForPhotoMock(page);
    await page.getByRole("button", { name: "ログアウト" }).click();
    await expect.poll(() => page.evaluate((settingsKey) => localStorage.getItem(settingsKey), PHOTO_SETTINGS_KEY)).toBeNull();
    await expect.poll(async () => (await page.evaluate(() => (window as unknown as { __chainPhotosMock?: { state: () => { pending: unknown[] } } }).__chainPhotosMock?.state().pending.length ?? 0))).toBe(0);
  });

  test("起動時に権限が取り消されていれば次回だけ Notice を出す", async ({ page }) => {
    await page.addInitScript(() => {
      (window as unknown as { __chainPhotosMockPermission: unknown }).__chainPhotosMockPermission = { photos: "denied" };
    });
    await preparePage(page, { autoShare: true, saveToDevice: false });
    await mockPhotoApi(page);
    await page.goto("/party/");
    // ドライバの権限確認が終わって通知フラグが立つまで待つ (完了前に遷移すると確認が中断される)。
    await expect.poll(() => page.evaluate(() => localStorage.getItem("chain.party.photos.permission-notice"))).toBe("1");
    await page.goto("/settings/");
    await expect(page.locator('[role="alert"]').filter({ hasText: "権限が無くなった" })).toBeVisible();
    await page.reload();
    await expect(page.locator('[role="alert"]').filter({ hasText: "権限が無くなった" })).toHaveCount(0);
  });

  test("他人の写真だけ保存し、保存記録で二重保存しない", async ({ page }) => {
    await preparePage(page, { autoShare: false, saveToDevice: true });
    await mockPhotoApi(page, {
      posts: { items: [{ uri: "p1", spaceId: PARTY.id, author: { did: "did:plc:other", handle: "other" }, createdAt: "2026-10-17T03:05:00Z", takenAt: "2026-10-17T03:05:00Z", media: [{ id: "other", url: "https://example.test/full", thumbUrl: "", alt: "", width: 1, height: 1 }] }, { uri: "p2", spaceId: PARTY.id, author: { did: "did:plc:testuser", handle: "test" }, createdAt: "2026-10-17T03:06:00Z", media: [{ id: "mine", url: "https://example.test/full", thumbUrl: "", alt: "", width: 1, height: 1 }] }, { uri: "p3", spaceId: PARTY.id, author: { did: "did:plc:other", handle: "other" }, moderation: { label: "block", display: "hide", categories: [] }, createdAt: "2026-10-17T03:07:00Z", media: [{ id: "hidden", url: "https://example.test/full", thumbUrl: "", alt: "", width: 1, height: 1 }] }] },
    });
    await page.goto("/party/");
    await waitForPhotoMock(page);
    await expect.poll(() => page.evaluate(() => (window as unknown as { __chainPhotosMock: { state: () => { saved: string[]; saveCalls: number } } }).__chainPhotosMock.state().saveCalls)).toBe(1);
    const callsBeforeReload = await page.evaluate(() => (window as unknown as { __chainPhotosMock: { calls: string[] } }).__chainPhotosMock.calls.length);
    await page.reload();
    await expect.poll(() => page.evaluate(() => (window as unknown as { __chainPhotosMock: { calls: string[] } }).__chainPhotosMock.calls.length)).toBe(callsBeforeReload);
  });

  test("終了 Notice の写真を保存で終了 party の写真を保存する", async ({ page }) => {
    await preparePage(page, { autoShare: false, saveToDevice: false });
    const options: { party?: unknown; posts: unknown } = {
      party: PARTY,
      posts: {
        items: [{ uri: "ended-post", spaceId: PARTY.id, author: { did: "did:plc:other", handle: "other" }, createdAt: "2026-10-17T03:05:00Z", takenAt: "2026-10-17T03:05:00Z", media: [{ id: "ended-media", url: "https://example.test/full", thumbUrl: "", alt: "", width: 1, height: 1 }] }],
      },
    };
    await mockPhotoApi(page, options);
    let socket: import("@playwright/test").WebSocketRoute | undefined;
    await page.routeWebSocket(/\/api\/v1\/ws$/, (ws) => {
      socket = ws;
      ws.onMessage((message) => {
        if (JSON.parse(String(message)).type === "auth") {
          ws.send(JSON.stringify({ type: "ready", did: "did:plc:testuser" }));
        }
      });
    });
    await page.goto("/party/");
    await expect.poll(() => socket !== undefined).toBeTruthy();
    options.party = null;
    socket?.send(JSON.stringify({ type: "event", event: "party.updated", space: PARTY.id, data: { id: PARTY.id, name: PARTY.name, phase: "ended", locked: false, present: [] } }));
    await expect(page.getByText("パーティが終わりました。メモリーに残っています")).toBeVisible();
    await page.getByRole("button", { name: "写真を保存" }).click();
    await waitForPhotoMock(page);
    await expect.poll(() => page.evaluate(() => (window as unknown as { __chainPhotosMock: { state: () => { saveCalls: number } } }).__chainPhotosMock.state().saveCalls)).toBe(1);
  });

  test("memories の異常応答でも party/settings/profile を描画する", async ({ page }) => {
    const options: { party: unknown; memories: unknown } = { party: null, memories: "404" };
    await page.addInitScript(() => {
      (window as unknown as { __chainPhotosMockPending: unknown[] }).__chainPhotosMockPending = [
        { mediaStoreId: "memory-check", takenAt: "2026-10-17T03:30:00Z", uri: "content://DCIM/Camera/memory-check" },
      ];
    });
    await preparePage(page, { autoShare: true, saveToDevice: false });
    await mockPhotoApi(page, options);
    for (const memory of ["404", {}, "broken"]) {
      options.memories = memory;
      await page.goto("/party/");
      await expect(page.getByText("今はパーティに参加していません")).toBeVisible();
    }
    await page.goto("/settings/");
    await expect(page.locator("header").getByText("設定", { exact: true })).toBeVisible();
    await page.goto("/profile/");
    await expect(page.locator("body")).toContainText("プロフィール");
  });
});
