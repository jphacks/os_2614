import {
  expect,
  test,
  type Page,
  type Request,
  type Route,
  type WebSocketRoute,
} from "@playwright/test";

// リアルタイム provider (todo 29) の連結を検証する。
// - signedIn のときだけ接続を 1 本張る (画面ごとに張らない)
// - 表示中画面 (group) が Space を購読 / 解除する
// - signedOut では接続しない
// - ログアウトで接続が閉じ、再接続しない
// WebSocket は page.routeWebSocket でモックする (実サーバ不要)。

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

type ApiMock = (route: Route, request: Request) => Promise<void> | void;

/** モックした WS 接続 1 本分の観測値。 */
type WsConnection = {
  url: string;
  /** ページから送られたフレーム (JSON 文字列)。 */
  messages: string[];
  /** ページ側から閉じられたか。 */
  closed: boolean;
};

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

/** JWT 風の文字列。session.ts は payload の iat / exp だけを読む。 */
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

/**
 * `/api/v1/ws` をモックし、各接続の観測値を connections に積む。auth を受けたら ready を返す。
 */
async function mockRealtime(page: Page, connections: WsConnection[]): Promise<void> {
  await page.routeWebSocket(/\/api\/v1\/ws$/, (ws: WebSocketRoute) => {
    const connection: WsConnection = { url: ws.url(), messages: [], closed: false };
    connections.push(connection);
    ws.onMessage((message) => {
      const text = String(message);
      connection.messages.push(text);
      const frame = JSON.parse(text) as { type?: string };
      if (frame.type === "auth") {
        ws.send(JSON.stringify({ type: "ready", did: "did:plc:testuser" }));
      }
    });
    ws.onClose(() => {
      connection.closed = true;
    });
  });
}

function framesOf(connection: WsConnection): { type?: string; space?: string; token?: string }[] {
  return connection.messages.map((message) => JSON.parse(message) as { type?: string });
}

test.describe("realtime provider", () => {
  test("signedIn opens exactly one connection and keeps it across navigation", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });
    const connections: WsConnection[] = [];
    await mockRealtime(page, connections);

    await page.goto("/");
    await expect(page.getByRole("navigation", { name: "メインナビゲーション" })).toBeVisible();

    await expect.poll(() => connections.length).toBe(1);
    expect(connections[0].url).toBe("wss://chain.i32.jp/api/v1/ws");

    await expect.poll(() => connections[0].messages.length).toBeGreaterThan(0);
    const auth = framesOf(connections[0]).find((frame) => frame.type === "auth");
    expect(auth?.token).toBeTruthy();

    // 画面を移動しても新しい接続は張らない。
    await page.getByRole("link", { name: "友達" }).click();
    await expect(page).toHaveURL(/\/friends\/?$/);
    await page.getByRole("link", { name: "プロフィール" }).click();
    await expect(page).toHaveURL(/\/profile\/?$/);
    await page.waitForTimeout(400);
    expect(connections).toHaveLength(1);
  });

  test("group screen subscribes to its space and unsubscribes when leaving", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, {
          id: "g1",
          name: "情報工学科 3年",
          visibility: "open",
          memberCount: 8,
          isMember: true,
          state: "active",
          radiusM: 40,
        });
        return;
      }
      if (url.pathname === "/api/v1/instances/g1/posts") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });
    const connections: WsConnection[] = [];
    await mockRealtime(page, connections);

    await page.goto("/group/?id=g1");

    await expect
      .poll(() => framesOf(connections[0] ?? { url: "", messages: [], closed: false }).some(
        (frame) => frame.type === "sub" && frame.space === "g1",
      ))
      .toBe(true);

    // 別画面へ移ると購読を解除する。
    await page.getByRole("link", { name: "友達" }).click();
    await expect(page).toHaveURL(/\/friends\/?$/);
    await expect
      .poll(() =>
        framesOf(connections[0]).some((frame) => frame.type === "unsub" && frame.space === "g1"),
      )
      .toBe(true);
  });

  test("signedOut visitor opens no connection", async ({ page }) => {
    const connections: WsConnection[] = [];
    await mockRealtime(page, connections);

    await page.goto("/");
    await page.waitForURL((url) => url.pathname.startsWith("/welcome"));
    await page.waitForTimeout(400);
    expect(connections).toHaveLength(0);
  });

  test("logout closes the connection and does not reconnect", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockApi(page, async (route, request) => {
      if (new URL(request.url()).pathname === "/api/v1/auth/logout") {
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });
    const connections: WsConnection[] = [];
    await mockRealtime(page, connections);

    await page.goto("/settings/");
    await expect.poll(() => connections.length).toBe(1);

    await page.getByRole("button", { name: "ログアウト", exact: true }).click();
    await page.waitForURL((url) => url.pathname.startsWith("/welcome"));

    await expect.poll(() => connections[0].closed).toBe(true);
    await page.waitForTimeout(400);
    expect(connections).toHaveLength(1);
  });
});
