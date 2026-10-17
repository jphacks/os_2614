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
type Captured = { method: string; path: string; body: unknown };

/** GET /groups/{id}/invites の 1 件 (service.InviteItem、09 §6)。 */
function inviteItem(status: string, approved: number, required: number) {
  return {
    id: "inv1",
    targetDid: "did:plc:tom",
    createdBy: "did:plc:alice",
    status,
    approvedCount: approved,
    requiredCount: required,
    createdAt: "2026-10-17T00:00:00Z",
    expiresAt: "2026-10-24T00:00:00Z",
  };
}

const INBOX = {
  items: [
    {
      id: "o1",
      spaceId: "g9",
      groupName: "ゼミ",
      inviter: { did: "did:plc:alice", handle: "alice.bsky.social", displayName: "アリス" },
      expiresAt: "2026-10-24T00:00:00Z",
    },
  ],
};

const GROUP_CLOSED = {
  id: "g1",
  name: "招待制のグループ",
  description: "",
  visibility: "closed",
  memberCount: 4,
  isMember: true,
  state: "active",
  radiusM: 40,
};

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

test.describe("closed-group invites", () => {
  test.beforeEach(async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
  });

  test("approving reaches the required count and offers the invite", async ({
    page,
  }) => {
    const calls: Captured[] = [];
    let approved = false;
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1/invites") {
        await fulfillJson(route, 200, {
          items: [approved ? inviteItem("offered", 2, 2) : inviteItem("collecting", 1, 2)],
        });
        return;
      }
      if (pathname === "/api/v1/groups/g1/invites/inv1/approve") {
        calls.push({ method, path: pathname, body: request.postDataJSON() });
        approved = true;
        await fulfillJson(route, 200, inviteItem("offered", 2, 2));
        return;
      }
      if (method === "GET" && pathname === "/api/v1/group-invites") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/invites/?id=g1");

    await expect(page.getByText("承認待ち")).toBeVisible();
    await expect(page.getByText("承認 1/2")).toBeVisible();

    await page.getByRole("button", { name: "承認" }).click();

    await expect
      .poll(() => calls.map((c) => c.path))
      .toContain("/api/v1/groups/g1/invites/inv1/approve");
    // 注記と副題も「提示済み」を含むため、状態タグの完全一致だけを検証する。
    await expect(page.getByText("提示済み", { exact: true })).toBeVisible();
    await expect(page.getByText("承認 2/2")).toBeVisible();
  });

  test("creating an invite posts the DID", async ({ page }) => {
    const calls: Captured[] = [];
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1/invites") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      if (method === "GET" && pathname === "/api/v1/group-invites") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      if (method === "POST" && pathname === "/api/v1/groups/g1/invites") {
        calls.push({ method, path: pathname, body: request.postDataJSON() });
        await fulfillJson(route, 201, inviteItem("collecting", 1, 2));
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/invites/?id=g1");
    await page.getByLabel("招待する相手の DID").fill("did:plc:tom");
    await page.getByRole("button", { name: "招待する" }).click();

    await expect.poll(() => calls.length).toBeGreaterThan(0);
    expect(calls[0].path).toBe("/api/v1/groups/g1/invites");
    expect(calls[0].body).toEqual({ did: "did:plc:tom" });
  });

  test("accepting an inbox invite posts to /group-invites/{id}/accept", async ({
    page,
  }) => {
    const calls: Captured[] = [];
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/group-invites") {
        await fulfillJson(route, 200, INBOX);
        return;
      }
      if (method === "POST" && pathname === "/api/v1/group-invites/o1/accept") {
        calls.push({ method, path: pathname, body: request.postDataJSON() });
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/invites/");

    await expect(page.getByText("ゼミ")).toBeVisible();
    await expect(page.getByText("アリスさんからの招待")).toBeVisible();

    await page.getByRole("button", { name: "参加" }).click();

    await expect
      .poll(() => calls.map((c) => c.path))
      .toContain("/api/v1/group-invites/o1/accept");
  });

  test("shows the reason when inviting a non-invitable DID (403)", async ({
    page,
  }) => {
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1/invites") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      if (method === "GET" && pathname === "/api/v1/group-invites") {
        await fulfillJson(route, 200, { items: [] });
        return;
      }
      if (method === "POST" && pathname === "/api/v1/groups/g1/invites") {
        await fulfillJson(route, 403, {
          error: { code: "forbidden", message: "招待できる相手ではありません" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/invites/?id=g1");
    await page.getByLabel("招待する相手の DID").fill("did:plc:stranger");
    await page.getByRole("button", { name: "招待する" }).click();

    await expect(page.getByText("招待できる相手ではありません")).toBeVisible();
  });

  test("closed group screen links to the invite management", async ({ page }) => {
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, GROUP_CLOSED);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/?id=g1");
    await expect(page.getByRole("link", { name: "グループの招待" })).toBeVisible();
  });
});
