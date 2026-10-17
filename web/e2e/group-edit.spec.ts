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

/** GET /groups/{id} の応答 (service.GroupDetail、09 §4)。role で owner / member を切替。 */
function groupDetail(role: string) {
  return {
    id: "g1",
    name: "情報工学科 3年",
    description: "3年生の情報交換の場です。",
    visibility: "closed",
    memberCount: 2,
    isMember: true,
    role,
    state: "active",
    radiusM: 40,
    inviteQuorum: 2,
  };
}

/** GET /groups/{id}/members の応答 (service.MemberItem、09 §4)。 */
const MEMBERS = {
  items: [
    {
      did: "did:plc:alice",
      handle: "alice.bsky.social",
      displayName: "アリス",
      role: "owner",
      joinedAt: "2026-01-01T00:00:00Z",
    },
    {
      did: "did:plc:bob",
      handle: "bob.bsky.social",
      displayName: "ボブ",
      role: "member",
      joinedAt: "2026-02-01T00:00:00Z",
    },
  ],
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

test.describe("group edit and members", () => {
  test.beforeEach(async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
  });

  test("lists the members with their role", async ({ page }) => {
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, groupDetail("owner"));
        return;
      }
      if (method === "GET" && pathname === "/api/v1/groups/g1/members") {
        await fulfillJson(route, 200, MEMBERS);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/?id=g1");
    await page.getByRole("radio", { name: "メンバー" }).click();

    await expect(page.getByText("アリス")).toBeVisible();
    await expect(page.getByText("@alice.bsky.social")).toBeVisible();
    await expect(page.getByText("オーナー")).toBeVisible();
    await expect(page.getByText("ボブ")).toBeVisible();
    await expect(page.getByText("@bob.bsky.social")).toBeVisible();

    // 投票・写真への入口はプレーンな href で置く。
    await expect(page.getByRole("link", { name: "投票" })).toHaveAttribute(
      "href",
      "/group/votes/?id=g1",
    );
    await expect(page.getByRole("link", { name: "写真を共有" })).toHaveAttribute(
      "href",
      "/group/photo/?id=g1",
    );
  });

  test("the owner sees the edit form and it PATCHes the group", async ({ page }) => {
    const calls: Captured[] = [];
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, groupDetail("owner"));
        return;
      }
      if (method === "GET" && pathname === "/api/v1/groups/g1/members") {
        await fulfillJson(route, 200, MEMBERS);
        return;
      }
      if (method === "PATCH" && pathname === "/api/v1/groups/g1") {
        calls.push({ method, path: pathname, body: request.postDataJSON() });
        await fulfillJson(route, 200, {
          ...groupDetail("owner"),
          name: "新しいグループ名",
          description: "更新した説明",
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/?id=g1");

    await page.getByRole("button", { name: "グループを編集" }).click();
    const dialog = page.getByRole("dialog", { name: "グループを編集" });
    await expect(dialog).toBeVisible();

    await page.getByLabel("グループ名").fill("新しいグループ名");
    await page.getByLabel("説明").fill("更新した説明");
    await page.getByLabel("承認が必要な人数").fill("3");
    await page.getByRole("button", { name: "保存" }).click();

    await expect(page.getByText("保存しました")).toBeVisible();
    await expect.poll(() => calls.length).toBe(1);
    expect(calls[0].method).toBe("PATCH");
    expect(calls[0].path).toBe("/api/v1/groups/g1");
    const body = calls[0].body as Record<string, unknown>;
    expect(body.name).toBe("新しいグループ名");
    expect(body.description).toBe("更新した説明");
    expect(body.visibility).toBe("closed");
    expect(body.inviteQuorum).toBe(3);
  });

  test("a non-owner does not see the edit affordance", async ({ page }) => {
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, groupDetail("member"));
        return;
      }
      if (method === "GET" && pathname === "/api/v1/groups/g1/members") {
        await fulfillJson(route, 200, MEMBERS);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/?id=g1");

    await expect(page.getByText("情報工学科 3年")).toBeVisible();
    await expect(page.getByRole("button", { name: "グループを編集" })).toHaveCount(0);
  });

  test("shows the message when the owner is not allowed to edit (403)", async ({
    page,
  }) => {
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (method === "GET" && pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, groupDetail("owner"));
        return;
      }
      if (method === "GET" && pathname === "/api/v1/groups/g1/members") {
        await fulfillJson(route, 200, MEMBERS);
        return;
      }
      if (method === "PATCH" && pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 403, {
          error: { code: "forbidden", message: "グループを編集する権限がありません" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/group/?id=g1");

    await page.getByRole("button", { name: "グループを編集" }).click();
    await page.getByLabel("グループ名").fill("変更後の名前");
    await page.getByRole("button", { name: "保存" }).click();

    await expect(
      page.getByText("グループを編集する権限がありません"),
    ).toBeVisible();
  });
});
