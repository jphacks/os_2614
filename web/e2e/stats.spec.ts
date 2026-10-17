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

/** GET /me/stats が返す statsResponse (13 §3、backend/internal/controller/me.go)。 */
const STATS = {
  encounters: { today: 12, week: 48, month: 160, total: 1024, uniquePeople: 380 },
  friends: 23,
  parties: { total: 15 },
  groups: { member: 6 },
  posts: { month: 210 },
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-22-chain-fe-be-gap-closure.png",
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

test.describe("stats", () => {
  test("renders the real GET /me/stats shape", async ({ page }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (new URL(request.url()).pathname === "/api/v1/me/stats") {
        await fulfillJson(route, 200, STATS);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/stats/");

    await expect(page.getByRole("heading", { name: "すれ違い" })).toBeVisible();
    // encounters.total は 3 桁区切りで表示する。
    await expect(page.getByText("1,024")).toBeVisible();
    await expect(page.getByText("160回")).toBeVisible(); // encounters.month
    await expect(page.getByText("23人")).toBeVisible(); // friends
    await expect(page.getByText("15回")).toBeVisible(); // parties.total
    await expect(page.getByText("210件")).toBeVisible(); // posts.month

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }
  });

  test("shows the unavailable state when GET /me/stats is 404", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/stats/");

    await expect(page.getByText("統計は準備中です")).toBeVisible();
  });

  test("shows the error state with a retry when GET /me/stats fails", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route) => {
      await fulfillJson(route, 500, {
        error: { code: "internal", message: "boom" },
      });
    });

    await page.goto("/stats/");

    await expect(page.getByText("統計を読み込めませんでした")).toBeVisible();
    await expect(page.getByRole("button", { name: "再読み込み" })).toBeVisible();
  });
});
