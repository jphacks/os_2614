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
  "access-control-allow-methods": "GET,POST,DELETE,OPTIONS",
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

/** GET /memories (13 §2) が返す service.MemoryItem。 */
const MEMORY = {
  spaceId: "space-1",
  type: "party",
  name: "10/17 13:05 の party",
  from: "2026-10-17T04:05:00Z",
  until: "2026-10-17T09:40:00Z",
  memberCount: 4,
  mediaCount: 37,
  cover: { thumbUrl: "" },
};

const MEMORY_PAGE_2 = {
  spaceId: "space-2",
  type: "party",
  name: "9/30 20:00 の party",
  from: "2026-09-30T11:00:00Z",
  until: "2026-09-30T14:00:00Z",
  memberCount: 3,
  mediaCount: 8,
  cover: { thumbUrl: "" },
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-19-chain-fe-be-gap-closure.png",
);

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

test.describe("memory", () => {
  test("renders an archived memory from GET /memories", async ({ page }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (
        request.method() === "GET" &&
        new URL(request.url()).pathname === "/api/v1/memories"
      ) {
        await fulfillJson(route, 200, { items: [MEMORY] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/memory/");

    await expect(page.getByText(MEMORY.name)).toBeVisible();
    await expect(page.getByText("4人・写真 37枚")).toBeVisible();
    await expect(page.getByText("まだメモリーはありません")).toHaveCount(0);

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }
  });

  test("shows the empty state when there are no memories", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (
        request.method() === "GET" &&
        new URL(request.url()).pathname === "/api/v1/memories"
      ) {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/memory/");

    await expect(page.getByText("まだメモリーはありません")).toBeVisible();
  });

  test("loads the next page with もっと見る when a cursor is returned", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (request.method() === "GET" && url.pathname === "/api/v1/memories") {
        if (url.searchParams.get("cursor") === "c1") {
          await fulfillJson(route, 200, { items: [MEMORY_PAGE_2] });
        } else {
          await fulfillJson(route, 200, { items: [MEMORY], cursor: "c1" });
        }
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/memory/");

    await expect(page.getByText(MEMORY.name)).toBeVisible();
    await expect(page.getByText(MEMORY_PAGE_2.name)).toHaveCount(0);

    await page.getByRole("button", { name: "もっと見る" }).click();

    await expect(page.getByText(MEMORY_PAGE_2.name)).toBeVisible();
    await expect(page.getByRole("button", { name: "もっと見る" })).toHaveCount(0);
  });

  test("shows 準備中 when DELETE /memories/{spaceId} is not implemented", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (request.method() === "GET" && url.pathname === "/api/v1/memories") {
        await fulfillJson(route, 200, { items: [MEMORY] });
        return;
      }
      if (
        request.method() === "DELETE" &&
        url.pathname === `/api/v1/memories/${MEMORY.spaceId}`
      ) {
        await fulfillJson(route, 404, {
          error: { code: "not_found", message: "not implemented" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/memory/");

    await page.getByRole("button", { name: "メモリーから隠す" }).click();

    await expect(
      page.getByText("非表示は準備中です。しばらくお待ちください。"),
    ).toBeVisible();
    // 隠せていないので、その memory は一覧に残ったまま。
    await expect(page.getByText(MEMORY.name)).toBeVisible();
  });

  test("shows the error state with a retry when GET /memories fails", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route) => {
      await fulfillJson(route, 500, {
        error: { code: "internal", message: "boom" },
      });
    });

    await page.goto("/memory/");

    await expect(
      page.getByText("メモリーを読み込めませんでした"),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "再読み込み" })).toBeVisible();
  });

  test("shows the unavailable state when GET /memories is 404", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/memory/");

    await expect(page.getByText("メモリーは準備中です")).toBeVisible();
  });
});
