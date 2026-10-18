import { expect, test } from "@playwright/test";
import { shouldSavePhoto } from "../src/device/photos/download";

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

async function preparePage(page: import("@playwright/test").Page, settings = { saveToDevice: false }) {
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
    if (path === "/api/v1/me/settings" || path === "/api/v1/me/location-sharing") {
      await route.fulfill({ status: 200, headers: { ...CORS_HEADERS, "content-type": "application/json" }, body: JSON.stringify(ME.settings) });
      return;
    }
    await route.fulfill({ status: 404, headers: { ...CORS_HEADERS, "content-type": "application/json" }, body: JSON.stringify({ error: { code: "not_found", message: "not found" } }) });
  });
}

async function waitForPhotoMock(page: import("@playwright/test").Page): Promise<void> {
  await page.waitForFunction(() => Boolean((window as unknown as { __chainPhotosMock?: { ready?: boolean } }).__chainPhotosMock?.ready));
}

test.describe("写真の端末保存の判定", () => {
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
});

test.describe("写真の端末保存の画面フロー", () => {
  test("他人の写真だけ保存し、保存記録で二重保存しない", async ({ page }) => {
    await preparePage(page, { saveToDevice: true });
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
    await preparePage(page, { saveToDevice: false });
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
});
