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

/** GET /groups/graph (service.GraphResult、09 §7)。中心座標は返らない。 */
const GRAPH = {
  nodes: [
    {
      id: "g1",
      name: "大学",
      radiusM: 800,
      memberCount: 120,
      isMember: true,
      inside: true,
      origin: "explicit",
    },
    {
      id: "g2",
      name: "教室",
      radiusM: 40,
      memberCount: 5,
      isMember: false,
      inside: true,
      origin: "explicit",
    },
  ],
  edges: [
    { from: "g1", to: "g2", kind: "contains" },
    { from: "g1", to: "g2", kind: "shared_members", weight: 3 },
  ],
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-40-chain-fe-be-gap-closure.png",
);

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

test.describe("group graph", () => {
  test.beforeEach(async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
  });

  test("renders nodes and edges from GET /groups/graph", async ({ page }, testInfo) => {
    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/groups/graph") {
        await fulfillJson(route, 200, GRAPH);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/graph/");

    await expect(page.getByText("大学", { exact: true }).first()).toBeVisible();
    await expect(page.getByText("教室", { exact: true }).first()).toBeVisible();
    await expect(page.getByText("半径 800m・メンバー 120人")).toBeVisible();
    await expect(page.getByText("メンバー", { exact: true })).toBeVisible();
    await expect(page.getByText("含む", { exact: true })).toBeVisible();
    await expect(page.getByText("共通メンバー", { exact: true })).toBeVisible();
    await expect(page.getByText("3人", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("img", { name: "グループの大きさ" }),
    ).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH, fullPage: true });
    }
  });

  test("shows the explanation for an empty graph", async ({ page }) => {
    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/groups/graph") {
        await fulfillJson(route, 200, { nodes: [], edges: [] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/graph/");

    await expect(page.getByText("近くにグループがありません")).toBeVisible();
  });

  test("shows the location CTA when GET /groups/graph is 403 location_stale", async ({
    page,
  }) => {
    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/groups/graph") {
        await fulfillJson(route, 403, {
          error: { code: "location_stale", message: "位置が古いです" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/graph/");

    await expect(
      page.getByText("位置情報がないとグループの関係を見られません"),
    ).toBeVisible();
  });

  test("shows the unavailable state when the endpoint is missing", async ({ page }) => {
    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/graph/");

    await expect(page.getByText("グループの関係は準備中です")).toBeVisible();
  });

  test("nearby screen links to the graph", async ({ page }) => {
    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/");

    await expect(
      page.getByRole("link", { name: "グループの関係" }),
    ).toBeVisible();
  });
});
