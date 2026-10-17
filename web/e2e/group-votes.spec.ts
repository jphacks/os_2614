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
type Captured = { method: string; path: string; body: unknown };

/** GET /groups/{id}/votes / POST 応答の 1 件 (service.VoteItem、09 §8.6)。 */
function voteItem(overrides: Record<string, unknown> = {}) {
  return {
    id: "v1",
    kind: "make_closed",
    status: "open",
    createdBy: "did:plc:alice",
    yesCount: 2,
    noCount: 1,
    requiredCount: 3,
    createdAt: "2026-10-17T00:00:00Z",
    closesAt: "2026-10-19T00:00:00Z",
    myBallot: null,
    ...overrides,
  };
}

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

const NOT_FOUND = { error: { code: "not_found", message: "not found" } };

test.describe("group voting", () => {
  test.beforeEach(async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
  });

  test("renders the vote list with counts", async ({ page }) => {
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1/votes") {
        await fulfillJson(route, 200, { items: [voteItem()] });
        return;
      }
      await fulfillJson(route, 404, NOT_FOUND);
    });

    await page.goto("/group/votes/?id=g1");

    await expect(page.getByText("受付中")).toBeVisible();
    await expect(page.getByText("賛成 2 / 反対 1")).toBeVisible();
    await expect(page.getByText("可決に必要な賛成 3")).toBeVisible();
    await expect(page.getByRole("button", { name: "賛成" })).toBeVisible();
    await expect(page.getByRole("button", { name: "反対" })).toBeVisible();
  });

  test("proposing posts the kind and refetches the list", async ({ page }) => {
    const calls: Captured[] = [];
    let proposed = false;
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1/votes") {
        await fulfillJson(route, 200, { items: proposed ? [voteItem()] : [] });
        return;
      }
      if (method === "POST" && pathname === "/api/v1/groups/g1/votes") {
        calls.push({ method, path: pathname, body: request.postDataJSON() });
        proposed = true;
        await fulfillJson(route, 201, voteItem());
        return;
      }
      await fulfillJson(route, 404, NOT_FOUND);
    });

    await page.goto("/group/votes/?id=g1");
    await expect(page.getByText("まだ投票はありません")).toBeVisible();

    await page.getByRole("button", { name: "招待制にする" }).click();

    await expect.poll(() => calls.length).toBeGreaterThan(0);
    expect(calls[0].path).toBe("/api/v1/groups/g1/votes");
    expect(calls[0].body).toEqual({ kind: "make_closed" });
    await expect(page.getByText("受付中")).toBeVisible();
  });

  test("casting a yes ballot posts {yes:true}", async ({ page }) => {
    const calls: Captured[] = [];
    let voted = false;
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1/votes") {
        await fulfillJson(route, 200, {
          items: [voteItem(voted ? { myBallot: true, yesCount: 3 } : {})],
        });
        return;
      }
      if (
        method === "POST" &&
        pathname === "/api/v1/groups/g1/votes/v1/ballot"
      ) {
        calls.push({ method, path: pathname, body: request.postDataJSON() });
        voted = true;
        await fulfillJson(route, 200, voteItem({ myBallot: true, yesCount: 3 }));
        return;
      }
      await fulfillJson(route, 404, NOT_FOUND);
    });

    await page.goto("/group/votes/?id=g1");
    await page.getByRole("button", { name: "賛成" }).click();

    await expect
      .poll(() => calls.map((c) => c.path))
      .toContain("/api/v1/groups/g1/votes/v1/ballot");
    expect(calls[0].body).toEqual({ yes: true });
    await expect(page.getByText("あなたは賛成に投票済み")).toBeVisible();
  });

  test("shows 準備中 when the votes endpoint is unavailable (404)", async ({
    page,
  }) => {
    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, NOT_FOUND);
    });

    await page.goto("/group/votes/?id=g1");

    await expect(page.getByText("投票は準備中です")).toBeVisible();
  });
});
