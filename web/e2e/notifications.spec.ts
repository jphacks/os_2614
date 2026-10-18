import { mkdirSync } from "node:fs";
import path from "node:path";
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

const NOTIFICATIONS = {
  items: [
    {
      id: "n1",
      type: "friend.request",
      data: { peer: { did: "did:plc:alice", handle: "alice.bsky.social" } },
      createdAt: "2026-10-05T09:00:00Z",
    },
    {
      id: "n2",
      type: "encounter.daily",
      data: { count: 3 },
      createdAt: "2026-10-04T09:00:00Z",
      readAt: "2026-10-04T10:00:00Z",
    },
  ],
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-32-chain-fe-be-gap-closure.png",
);

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

function fakeJwt(claims: Record<string, unknown>): string {
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const payload = base64url(JSON.stringify(claims));
  return `${header}.${payload}.signature`;
}

function jwtFor(did: string): string {
  const now = Math.floor(Date.now() / 1000);
  return fakeJwt({ sub: did, iat: now, exp: now + 900 });
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

test.describe("notifications", () => {
  test("renders notifications and marks a row read", async ({ page }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    let readBody: unknown;
    let deviceCalls = 0;

    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (pathname === "/api/v1/notifications" && method === "GET") {
        await fulfillJson(route, 200, NOTIFICATIONS);
        return;
      }
      if (pathname === "/api/v1/notifications/read" && method === "POST") {
        readBody = request.postDataJSON();
        await fulfillJson(route, 200, { updated: 1 });
        return;
      }
      if (pathname.startsWith("/api/v1/me/devices")) {
        deviceCalls += 1;
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/notifications/");

    await expect(page.getByText("フレンド申請が届きました")).toBeVisible();
    await expect(page.getByText("今日のすれ違い")).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }

    await page.getByText("フレンド申請が届きました").click();

    await expect.poll(() => readBody).toEqual({ ids: ["n1"] });
    // 通知の内容に応じた画面へ遷移する。
    await expect(page).toHaveURL(/\/friends\/requests\/$/);
    // ブラウザは Android ではないので端末登録をしてはいけない。
    expect(deviceCalls).toBe(0);
  });

  test("marks all read with {all:true}", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    let readBody: unknown;
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (pathname === "/api/v1/notifications" && method === "GET") {
        await fulfillJson(route, 200, NOTIFICATIONS);
        return;
      }
      if (pathname === "/api/v1/notifications/read" && method === "POST") {
        readBody = request.postDataJSON();
        await fulfillJson(route, 200, { updated: 2 });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/notifications/");
    await page.getByRole("button", { name: "すべて既読" }).click();

    await expect.poll(() => readBody).toEqual({ all: true });
  });

  test("shows the empty explanation when there are no notifications", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/notifications") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/notifications/");

    await expect(page.getByText("通知はありません")).toBeVisible();
  });
});

test.describe("notifications unavailable state", () => {
  test("shows 準備中 and no rows when GET /notifications is 404", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/notifications/");

    await expect(page.getByText("通知は準備中です")).toBeVisible();
    await expect(page.getByText("通知はありません")).toHaveCount(0);
  });
});
