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

/** GET /friends/requests worker (service.FriendRequestItem、07 §2.2)。 */
const INCOMING = {
  items: [
    {
      id: "req-alice",
      peer: { did: "did:plc:alice", handle: "alice.bsky.social", displayName: "アリス" },
      via: "encounter",
      viaRef: "enc-1",
      status: "pending",
      createdAt: "2026-10-17T00:00:00Z",
    },
  ],
};

const OUTGOING = {
  items: [
    {
      id: "req-bob",
      peer: { did: "did:plc:bob", handle: "bob.bsky.social", displayName: "ボブ" },
      via: "space",
      viaRef: "g1",
      status: "pending",
      createdAt: "2026-10-17T00:00:00Z",
    },
  ],
};

const INVITE = {
  code: "7KQ2-M9XD",
  url: "chain://friend/7KQ2M9XD",
  expiresAt: "2026-10-17T03:22:45Z",
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-33-chain-fe-be-gap-closure.png",
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

/** GET /friends/requests を box ごとに返し、他の API は 404 にする既定モック。 */
async function mockRequests(
  page: Page,
  overrides: { acceptStatus?: number; acceptInviteStatus?: number } = {},
): Promise<{ posts: string[] }> {
  const posts: string[] = [];
  await mockApi(page, async (route, request) => {
    const url = new URL(request.url());
    const method = request.method();
    const pathname = apiPath(request.url());

    if (method === "GET" && pathname === "/api/v1/friends/requests") {
      const box = url.searchParams.get("box");
      await fulfillJson(route, 200, box === "outgoing" ? OUTGOING : INCOMING);
      return;
    }
    if (method === "POST" && pathname === "/api/v1/friends/requests/req-alice/accept") {
      posts.push(pathname);
      if (overrides.acceptStatus && overrides.acceptStatus >= 400) {
        await fulfillJson(route, overrides.acceptStatus, {
          error: { code: "forbidden", message: "関係がありません" },
        });
        return;
      }
      await fulfillJson(route, 200, { friend: INCOMING.items[0].peer });
      return;
    }
    if (method === "POST" && pathname === "/api/v1/friends/invites") {
      posts.push(pathname);
      await fulfillJson(route, 201, INVITE);
      return;
    }
    if (
      method === "POST" &&
      pathname === `/api/v1/friends/invites/${INVITE.code}/accept`
    ) {
      posts.push(pathname);
      if (overrides.acceptInviteStatus && overrides.acceptInviteStatus >= 400) {
        await fulfillJson(route, overrides.acceptInviteStatus, {
          error: { code: "forbidden", message: "ブロックされています" },
        });
        return;
      }
      await fulfillJson(route, 200, { friend: { did: "did:plc:carol" } });
      return;
    }
    await fulfillJson(route, 404, {
      error: { code: "not_found", message: "not found" },
    });
  });
  return { posts };
}

test.describe("friend requests", () => {
  test.beforeEach(async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
  });

  test("lists incoming and outgoing requests", async ({ page }, testInfo) => {
    await mockRequests(page);
    await page.goto("/friends/requests/");

    await expect(page.getByText("受け取った申請")).toBeVisible();
    await expect(page.getByText("アリス", { exact: true })).toBeVisible();
    await expect(page.getByText("@alice.bsky.social・すれ違いから")).toBeVisible();
    await expect(page.getByText("送った申請")).toBeVisible();
    await expect(page.getByText("ボブ", { exact: true })).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH, fullPage: true });
    }
  });

  test("accepting a request posts to /friends/requests/{id}/accept", async ({
    page,
  }) => {
    const { posts } = await mockRequests(page);
    await page.goto("/friends/requests/");
    await expect(page.getByText("アリス", { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "承認" }).click();

    await expect
      .poll(() => posts)
      .toContain("/api/v1/friends/requests/req-alice/accept");
  });

  test("creating an invite shows the code and posts to /friends/invites", async ({
    page,
  }) => {
    const { posts } = await mockRequests(page);
    await page.goto("/friends/requests/");

    await page.getByRole("button", { name: "コードを作る" }).click();

    await expect(page.getByText("7KQ2-M9XD")).toBeVisible();
    await expect.poll(() => posts).toContain("/api/v1/friends/invites");
  });

  test("accepting an invite code posts to /friends/invites/{code}/accept", async ({
    page,
  }) => {
    const { posts } = await mockRequests(page);
    await page.goto("/friends/requests/");

    await page.getByLabel("コードを受け取った").fill("7KQ2-M9XD");
    await page.getByRole("button", { name: "追加" }).click();

    await expect
      .poll(() => posts)
      .toContain("/api/v1/friends/invites/7KQ2-M9XD/accept");
    await expect(page.getByText("友達になりました。")).toBeVisible();
  });

  test("shows the reason when the invite is forbidden", async ({ page }) => {
    await mockRequests(page, { acceptInviteStatus: 403 });
    await page.goto("/friends/requests/");

    await page.getByLabel("コードを受け取った").fill("7KQ2-M9XD");
    await page.getByRole("button", { name: "追加" }).click();

    await expect(page.getByText("この相手とは友達になれません。")).toBeVisible();
  });

  test("shows the request action error when accept fails", async ({ page }) => {
    await mockRequests(page, { acceptStatus: 403 });
    await page.goto("/friends/requests/");

    await page.getByRole("button", { name: "承認" }).click();

    await expect(page.getByText("関係がありません")).toBeVisible();
  });
});
