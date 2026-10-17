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

const PAGE_ONE = {
  items: [
    {
      id: "e1",
      peer: { did: "did:plc:alice", handle: "alice.bsky.social", displayName: "アリス" },
      seenFrom: "2026-10-03T09:00:00Z",
      seenUntil: "2026-10-03T09:30:00Z",
      timesWithPeer: 2,
      relation: "friend",
    },
  ],
  cursor: "c2",
};

const PAGE_TWO = {
  items: [
    {
      id: "e2",
      peer: { did: "did:plc:bob", handle: "bob.bsky.social", displayName: "ボブ" },
      seenFrom: "2026-10-04T09:00:00Z",
      seenUntil: "2026-10-04T09:30:00Z",
      timesWithPeer: 1,
      relation: "none",
    },
  ],
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-31-chain-fe-be-gap-closure.png",
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

test.describe("encounters", () => {
  test("renders revealed encounters and appends the next page", async ({
    page,
  }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/encounters") {
        const cursor = new URL(request.url()).searchParams.get("cursor");
        await fulfillJson(route, 200, cursor === "c2" ? PAGE_TWO : PAGE_ONE);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/encounters/");

    await expect(page.getByText("アリス", { exact: true })).toBeVisible();
    await expect(page.getByText("友達", { exact: true })).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }

    await page.getByRole("button", { name: "もっと見る" }).click();

    await expect(page.getByText("ボブ", { exact: true })).toBeVisible();
    await expect(page.getByText("アリス", { exact: true })).toBeVisible();
  });

  test("shows the empty explanation when there are no encounters", async ({
    page,
  }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      if (apiPath(request.url()) === "/api/v1/encounters") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/encounters/");

    await expect(page.getByText("まだすれ違いはありません")).toBeVisible();
  });
});
