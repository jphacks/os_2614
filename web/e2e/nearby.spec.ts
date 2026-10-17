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

/** GET /groups/nearby (09 §5) の NearbyResult。inside は距離なし、near は帯のみ。 */
const NEARBY = {
  inside: [
    {
      id: "g1",
      name: "情報工学科 3年",
      memberCount: 12,
      isMember: false,
      persistent: true,
      radiusM: 120,
    },
  ],
  near: [
    {
      id: "g2",
      name: "朝のランニング部",
      memberCount: 5,
      distanceBucket: "<500m",
    },
  ],
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-30-chain-fe-be-gap-closure.png",
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

test.describe("nearby", () => {
  test("renders inside and near groups from GET /groups/nearby", async ({
    page,
  }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/groups/nearby") {
        await fulfillJson(route, 200, NEARBY);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");

    await expect(page.getByText("情報工学科 3年", { exact: true })).toBeVisible();
    await expect(page.getByText("朝のランニング部", { exact: true })).toBeVisible();
    await expect(page.getByText("<500m", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "参加" })).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }
  });

  test("join issues POST /groups/{id}/join and reloads", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    let nearbyCalls = 0;
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/nearby") {
        nearbyCalls += 1;
        await fulfillJson(route, 200, NEARBY);
        return;
      }
      if (method === "POST" && pathname === "/api/v1/groups/g1/join") {
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");
    await expect(page.getByRole("button", { name: "参加" })).toBeVisible();

    const joinRequest = page.waitForRequest(
      (request) =>
        request.method() === "POST" &&
        apiPath(request.url()) === "/api/v1/groups/g1/join",
    );

    await page.getByRole("button", { name: "参加" }).click();

    const request = await joinRequest;
    expect(apiPath(request.url())).toBe("/api/v1/groups/g1/join");
    await expect.poll(() => nearbyCalls).toBeGreaterThan(1);
  });

  test("shows the location CTA when GET /groups/nearby is 403 location_stale", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/groups/nearby") {
        await fulfillJson(route, 403, {
          error: { code: "location_stale", message: "位置が古いです" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");

    await expect(
      page.getByText("グループを見るには位置情報が必要です"),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "位置を有効にする" }),
    ).toBeVisible();
  });

  test("shows the empty explanation when nothing is nearby", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/groups/nearby") {
        await fulfillJson(route, 200, { inside: [], near: [] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");

    await expect(page.getByText("近くのグループはありません")).toBeVisible();
  });
});

test.describe("nearby unavailable state", () => {
  test("shows 準備中 and no rows when GET /groups/nearby is 404", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");

    await expect(page.getByText("ちかくは準備中です")).toBeVisible();
    await expect(page.getByText("いまいるエリア")).toHaveCount(0);
    await expect(page.getByText("近くのグループはありません")).toHaveCount(0);
  });
});
