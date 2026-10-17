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
  "access-control-allow-methods": "GET,POST,OPTIONS",
  "access-control-allow-headers": "authorization,content-type",
  "access-control-max-age": "600",
};

type StoredSession = {
  did: string;
  accessToken: string;
  refreshToken: string;
  accessExpiresAtMs: number;
};

type TokensResponse = {
  did: string;
  accessToken: string;
  accessExpiresAt: string;
  refreshToken: string;
};

type ApiMock = (route: Route, request: Request) => Promise<void> | void;

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

/** JWT 風の文字列。session.ts は payload の iat / exp だけを読む。 */
function fakeJwt(claims: Record<string, unknown>): string {
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const payload = base64url(JSON.stringify(claims));
  return `${header}.${payload}.signature`;
}

function jwtFor(did: string): string {
  const now = Math.floor(Date.now() / 1000);
  return fakeJwt({ sub: did, iat: now, exp: now + 900 });
}

function makeTokens(accessToken?: string): TokensResponse {
  const did = "did:plc:testuser";
  const now = Math.floor(Date.now() / 1000);
  return {
    did,
    accessToken: accessToken ?? jwtFor(did),
    accessExpiresAt: new Date((now + 900) * 1000).toISOString(),
    refreshToken: "refresh-token-1",
  };
}

function makeStoredSession(expiresInMs = 60 * 60 * 1000): StoredSession {
  const did = "did:plc:testuser";
  return {
    did,
    accessToken: jwtFor(did),
    refreshToken: "refresh-token-0",
    accessExpiresAtMs: Date.now() + expiresInMs,
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

/** chain.i32.jp への API 呼び出しをモックする。CORS プリフライトも同じ場所で返す。 */
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

async function readStoredSession(page: Page): Promise<StoredSession | null> {
  return page.evaluate((key) => {
    const raw = window.localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as StoredSession) : null;
  }, SESSION_KEY);
}

test.describe("signed in", () => {
  test.beforeEach(async ({ page }) => {
    // AuthGate を通すため、期限が 1 時間先のダミーセッションを入れておく。
    await useStoredSession(page, makeStoredSession());
  });

  test("nearby screen and tab bar render", async ({ page }) => {
    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");

    await expect(page.getByText("近くのグループ")).toBeVisible();
    await expect(
      page.getByRole("navigation", { name: "メインナビゲーション" }),
    ).toBeVisible();
  });

  test("tab navigation reaches friends screen", async ({ page }) => {
    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");

    await page.getByRole("link", { name: "友達" }).click();

    await expect(page).toHaveURL(/\/friends\/?$/);
    await expect(
      page.getByText("位置はエリア名だけ共有しています"),
    ).toBeVisible();
  });
});

test.describe("auth", () => {
  test("signed-out visitor is sent from / to /welcome/", async ({ page }) => {
    await page.goto("/");

    await page.waitForURL((url) => url.pathname.startsWith("/welcome"));
    await expect(
      page.getByRole("button", { name: "Blueskyで続ける" }),
    ).toBeVisible();
  });

  test("callback exchanges the code and stores the session", async ({ page }) => {
    const tokens = makeTokens();
    let exchangeBody: { code?: string; deviceName?: string } | null = null;

    await mockApi(page, async (route, request) => {
      if (new URL(request.url()).pathname === "/api/v1/auth/exchange") {
        exchangeBody = request.postDataJSON() as {
          code?: string;
          deviceName?: string;
        };
        await fulfillJson(route, 200, tokens);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/auth/callback/?code=abc");
    await page.waitForURL((url) => url.pathname === "/");

    expect(exchangeBody).toMatchObject({ code: "abc" });

    const stored = await readStoredSession(page);
    expect(stored?.did).toBe(tokens.did);
    expect(stored?.accessToken).toBe(tokens.accessToken);
    expect(stored?.refreshToken).toBe(tokens.refreshToken);
    expect(stored?.accessExpiresAtMs).toBeGreaterThan(Date.now());

    // 保存された access token は header.payload.signature 形式。
    expect(tokens.accessToken.split(".")).toHaveLength(3);
    await expect(page.getByText("近くのグループ")).toBeVisible();
  });

  test("expired exchange shows the re-login action", async ({ page }) => {
    await mockApi(page, async (route, request) => {
      if (new URL(request.url()).pathname === "/api/v1/auth/exchange") {
        await fulfillJson(route, 401, {
          error: { code: "unauthenticated", message: "one-time code expired" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/auth/callback/?code=expired");

    await expect(page.getByText("ログインの有効期限が切れました")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "もう一度ログイン" }),
    ).toBeVisible();
  });

  test("callback error parameter is shown with the re-login action", async ({
    page,
  }) => {
    await page.goto("/auth/callback/?error=access_denied");

    await expect(page.getByText("ログインできませんでした")).toBeVisible();
    await expect(page.getByText("access_denied")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "もう一度ログイン" }),
    ).toBeVisible();
  });

  test("logout calls the API with the bearer token and clears the session", async ({
    page,
  }) => {
    const session = makeStoredSession();
    await useStoredSession(page, session);
    let logoutAuthorization: string | undefined;

    await mockApi(page, async (route, request) => {
      if (new URL(request.url()).pathname === "/api/v1/auth/logout") {
        logoutAuthorization = request.headers()["authorization"];
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/settings/");
    await page.getByRole("button", { name: "ログアウト", exact: true }).click();

    await page.waitForURL((url) => url.pathname.startsWith("/welcome"));
    expect(logoutAuthorization).toBe(`Bearer ${session.accessToken}`);
    await expect
      .poll(() => readStoredSession(page))
      .toBeNull();
  });

  test("expired access token is refreshed once before logout", async ({ page }) => {
    const expired = makeStoredSession(-60_000);
    await useStoredSession(page, expired);
    const refreshed = makeTokens();
    let refreshCalls = 0;
    let refreshBody: unknown;
    let logoutAuthorization: string | undefined;

    await mockApi(page, async (route, request) => {
      const path = new URL(request.url()).pathname;
      if (path === "/api/v1/auth/refresh") {
        refreshCalls += 1;
        refreshBody = request.postDataJSON();
        await fulfillJson(route, 200, refreshed);
        return;
      }
      if (path === "/api/v1/auth/logout") {
        logoutAuthorization = request.headers()["authorization"];
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/settings/");
    await page.getByRole("button", { name: "ログアウト", exact: true }).click();

    await page.waitForURL((url) => url.pathname.startsWith("/welcome"));
    expect(refreshCalls).toBe(1);
    expect(refreshBody).toEqual({ refreshToken: expired.refreshToken });
    expect(logoutAuthorization).toBe(`Bearer ${refreshed.accessToken}`);
    await expect.poll(() => readStoredSession(page)).toBeNull();
  });
});
