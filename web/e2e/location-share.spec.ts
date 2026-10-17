import {
  expect,
  test,
  type Page,
  type Request,
  type Route,
} from "@playwright/test";

const API_BASE = "https://chain.i32.jp";
const SESSION_KEY = "chain.auth.session.v1";

const CORS_HEADERS: Record<string, string> = {
  "access-control-allow-origin": "*",
  "access-control-allow-methods": "GET,POST,PATCH,PUT,DELETE,OPTIONS",
  "access-control-allow-headers": "authorization,content-type",
  "access-control-max-age": "600",
};

type StoredSession = {
  did: string;
  accessToken: string;
  refreshToken: string;
  accessExpiresAtMs: number;
};

type ApiMock = (route: Route, request: Request) => Promise<void> | void;
type CapturedPut = { path: string; body: unknown };

/** GET /friends (service.FriendItem、07 §2.3)。precision は自分が相手に設定した値。 */
const FRIENDS = {
  items: [
    {
      did: "did:plc:alice",
      handle: "alice.bsky.social",
      displayName: "アリス",
      since: "2026-09-01T00:00:00Z",
      precision: "area",
    },
    {
      did: "did:plc:bob",
      handle: "bob.bsky.social",
      displayName: "ボブ",
      since: "2026-09-02T00:00:00Z",
      precision: "direction",
    },
  ],
};

/** GET /friends/locations (controller.friendLocationResponse、07 §4.2)。 */
const LOCATIONS = {
  items: [
    {
      did: "did:plc:alice",
      precision: "area",
      area: { label: "大学" },
      updatedAt: "2026-10-17T03:12:00Z",
    },
    {
      did: "did:plc:bob",
      precision: "direction",
      direction: "N",
      distanceBucket: "<500m",
      updatedAt: "2026-10-17T03:12:00Z",
    },
  ],
};

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

function fakeJwt(claims: Record<string, unknown>): string {
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const payload = base64url(JSON.stringify(claims));
  return `${header}.${payload}.signature`;
}

function makeStoredSession(): StoredSession {
  const did = "did:plc:testuser";
  const now = Math.floor(Date.now() / 1000);
  return {
    did,
    accessToken: fakeJwt({ sub: did, iat: now, exp: now + 900 }),
    refreshToken: "refresh-token-0",
    accessExpiresAtMs: Date.now() + 60 * 60 * 1000,
  };
}

async function useStoredSession(page: Page, session: StoredSession): Promise<void> {
  await page.addInitScript(
    ({ key, value }) => {
      window.localStorage.setItem(key, value);
    },
    { key: SESSION_KEY, value: JSON.stringify(session) },
  );
}

async function fulfillJson(route: Route, status: number, body: unknown): Promise<void> {
  await route.fulfill({
    status,
    headers: { ...CORS_HEADERS, "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}

async function mockApi(page: Page, mock: ApiMock): Promise<void> {
  await page.route(`${API_BASE}/**`, async (route) => {
    const request = route.request();
    if (request.method() === "OPTIONS") {
      await route.fulfill({ status: 204, headers: CORS_HEADERS });
      return;
    }
    await mock(route, request);
  });
}

function apiPath(url: string): string {
  return decodeURIComponent(new URL(url).pathname);
}

async function mockFriends(
  page: Page,
  opts: { locations?: unknown; putStatus?: number } = {},
): Promise<CapturedPut[]> {
  const puts: CapturedPut[] = [];
  await mockApi(page, async (route, request) => {
    const method = request.method();
    const pathname = apiPath(request.url());
    if (method === "GET" && pathname === "/api/v1/friends") {
      await fulfillJson(route, 200, FRIENDS);
      return;
    }
    if (method === "GET" && pathname === "/api/v1/friends/locations") {
      await fulfillJson(route, 200, opts.locations ?? LOCATIONS);
      return;
    }
    if (method === "PUT" && pathname.endsWith("/location-share")) {
      puts.push({ path: pathname, body: request.postDataJSON() });
      if (opts.putStatus && opts.putStatus >= 400) {
        await fulfillJson(route, opts.putStatus, {
          error: { code: "internal", message: "boom" },
        });
        return;
      }
      await route.fulfill({ status: 204, headers: CORS_HEADERS });
      return;
    }
    await fulfillJson(route, 404, {
      error: { code: "not_found", message: "not found" },
    });
  });
  return puts;
}

test.describe("per-friend location sharing", () => {
  test.beforeEach(async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
  });

  test("shows each friend's location from GET /friends/locations", async ({
    page,
  }) => {
    await mockFriends(page);
    await page.goto("/friends/");

    await expect(page.getByText("アリス", { exact: true })).toBeVisible();
    await expect(page.getByText("大学", { exact: true })).toBeVisible();
    await expect(page.getByText("N <500m", { exact: true })).toBeVisible();
    // locations が ok なので「準備中」は出ない。
    await expect(page.getByText("位置は準備中")).toHaveCount(0);
  });

  test("hides a stale friend location", async ({ page }) => {
    await mockFriends(page, {
      locations: {
        items: [
          { did: "did:plc:alice", precision: "area", area: { label: "大学" } },
          { did: "did:plc:bob", precision: "direction", stale: true },
        ],
      },
    });
    await page.goto("/friends/");

    await expect(page.getByText("大学", { exact: true })).toBeVisible();
    // stale なボブは位置を出さない (方角も距離も出ない)。
    await expect(page.getByText("N <500m")).toHaveCount(0);
  });

  test("changing the precision PUTs /friends/{did}/location-share", async ({
    page,
  }) => {
    const puts = await mockFriends(page);
    await page.goto("/friends/");
    await expect(page.getByText("アリス", { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "アリスの位置の見せ方" }).click();
    await page.getByRole("button", { name: "位置を共有しない" }).click();

    await expect.poll(() => puts.length).toBeGreaterThan(0);
    expect(puts[0].path).toBe("/api/v1/friends/did:plc:alice/location-share");
    expect(puts[0].body).toEqual({ precision: "off", areasOnly: false });
  });

  test("shows an error and keeps the previous precision when the PUT fails", async ({
    page,
  }) => {
    await mockFriends(page, { putStatus: 500 });
    await page.goto("/friends/");
    await expect(page.getByText("アリス", { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "アリスの位置の見せ方" }).click();
    await page.getByRole("button", { name: "位置を共有しない" }).click();

    await expect(
      page.getByText("位置の見せ方を変更できませんでした。"),
    ).toBeVisible();
  });
});
