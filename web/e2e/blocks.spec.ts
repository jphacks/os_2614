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

const BLOCKS = {
  items: [
    { did: "did:plc:alice", handle: "alice.bsky.social", displayName: "アリス" },
  ],
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-34-chain-fe-be-gap-closure.png",
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

test.describe("blocks", () => {
  test("lists blocks, adds by DID and removes", async ({ page }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    let addBody: unknown;
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/blocks") {
        await fulfillJson(route, 200, BLOCKS);
        return;
      }
      if (method === "POST" && pathname === "/api/v1/blocks") {
        addBody = request.postDataJSON();
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }
      if (method === "DELETE" && pathname === "/api/v1/blocks/did:plc:alice") {
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/blocks/");

    await expect(page.getByText("アリス", { exact: true })).toBeVisible();
    await expect(page.getByText("@alice.bsky.social", { exact: true })).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }

    await page.getByLabel("ブロックする相手の DID").fill("did:plc:carol");
    await page.getByRole("button", { name: "追加" }).click();
    await expect.poll(() => addBody).toEqual({ did: "did:plc:carol" });

    const deleteRequest = page.waitForRequest(
      (request) =>
        request.method() === "DELETE" &&
        apiPath(request.url()) === "/api/v1/blocks/did:plc:alice",
    );
    await page.getByRole("button", { name: "解除" }).click();
    await deleteRequest;
  });

  test("shows a message when the user is already blocked (409)", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/blocks") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      if (method === "POST" && pathname === "/api/v1/blocks") {
        await fulfillJson(route, 409, {
          error: { code: "conflict", message: "すでにブロックしています" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/blocks/");
    await page.getByLabel("ブロックする相手の DID").fill("did:plc:carol");
    await page.getByRole("button", { name: "追加" }).click();

    await expect(
      page.getByText("この相手はすでにブロックしています。"),
    ).toBeVisible();
  });

  test("shows the empty explanation when nothing is blocked", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/blocks") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/blocks/");

    await expect(page.getByText("ブロックした人はいません")).toBeVisible();
  });
});

test.describe("blocks unavailable state", () => {
  test("shows 準備中 and no rows when GET /blocks is 404", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/blocks/");

    await expect(page.getByText("ブロックは準備中です")).toBeVisible();
    await expect(page.getByText("ブロックした人はいません")).toHaveCount(0);
  });
});
