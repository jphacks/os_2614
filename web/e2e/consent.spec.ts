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
  "access-control-allow-methods": "GET,POST,PUT,PATCH,DELETE,OPTIONS",
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

function me(consentVersion?: number) {
  return {
    did: "did:plc:testuser",
    handle: "test.bsky.social",
    displayName: "てすとたろう",
    description: "",
    settings: {
      encounterMode: "delayed",
      encounterDelaySec: 3600,
      friendBeaconShare: true,
      partyAuto: true,
      locationPaused: false,
      warnDisplay: "blur",
      ...(consentVersion === undefined ? {} : { contentScanConsentVersion: consentVersion }),
      notify: {},
    },
  };
}

const GROUP_INSTANCE = {
  spaceId: "g1",
  type: "group",
  name: "情報科学演習 A",
  state: "active",
  memberCount: 3,
  canWrite: true,
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-35-chain-fe-be-gap-closure.png",
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

function makeStoredSession(): StoredSession {
  const did = "did:plc:testuser";
  return {
    did,
    accessToken: jwtFor(did),
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

test.describe("consent gate", () => {
  test("blocks posting until the content-scan consent is given", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const pathname = apiPath(request.url());
      if (pathname === "/api/v1/me") {
        await fulfillJson(route, 200, me(undefined));
        return;
      }
      if (pathname === "/api/v1/instances") {
        await fulfillJson(route, 200, { items: [GROUP_INSTANCE] });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/compose/");
    await page.getByLabel("投稿本文").fill("同意が必要な投稿");

    await expect(
      page.getByText("投稿には内容チェックへの同意が必要です"),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "投稿する" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "同意する" })).toBeVisible();
  });

  test("agree -> PUT /me/consents -> posting becomes available", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    let consentVersion: number | undefined;
    let putBody: unknown;
    let postBody: unknown;

    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (pathname === "/api/v1/me" && method === "GET") {
        await fulfillJson(route, 200, me(consentVersion));
        return;
      }
      if (pathname === "/api/v1/me/consents" && method === "PUT") {
        putBody = request.postDataJSON();
        consentVersion = 1;
        await fulfillJson(route, 200, {
          contentScan: 1,
          consentedAt: "2026-10-10T00:00:00Z",
        });
        return;
      }
      if (pathname === "/api/v1/instances" && method === "GET") {
        await fulfillJson(route, 200, { items: [GROUP_INSTANCE] });
        return;
      }
      if (pathname === "/api/v1/posts" && method === "POST") {
        postBody = request.postDataJSON();
        await fulfillJson(route, 201, {
          items: [{ uri: "at://did:plc:testuser/app.bsky.feed.post/1", spaceId: "g1" }],
        });
        return;
      }
      if (pathname === "/api/v1/groups/g1") {
        await fulfillJson(route, 200, {
          id: "g1",
          name: GROUP_INSTANCE.name,
          description: "",
          visibility: "open",
          memberCount: 3,
          isMember: true,
          state: "active",
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/consent/?next=compose");
    await expect(
      page.getByText("まだ同意していません"),
    ).toBeVisible();

    await page.getByRole("button", { name: "同意する" }).click();
    await expect.poll(() => putBody).toEqual({ contentScan: 1 });

    await page.waitForURL((url) => url.pathname.startsWith("/compose"));
    const textarea = page.getByLabel("投稿本文");
    await expect(textarea).toBeVisible({ timeout: 15_000 });
    await textarea.fill("同意後の投稿");

    const submit = page.getByRole("button", { name: "投稿する" });
    await expect(submit).toBeEnabled();

    if (test.info().project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }

    await submit.click();
    await expect.poll(() => postBody).not.toBeNull();
    await expect(page).toHaveURL(/\/group\/\?id=g1$/);
  });

  test("withdrawing sends contentScan:0", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    let putBody: unknown;
    await mockApi(page, async (route, request) => {
      const method = request.method();
      const pathname = apiPath(request.url());
      if (pathname === "/api/v1/me" && method === "GET") {
        await fulfillJson(route, 200, me(1));
        return;
      }
      if (pathname === "/api/v1/me/consents" && method === "PUT") {
        putBody = request.postDataJSON();
        await fulfillJson(route, 200, { contentScan: 0, consentedAt: null });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/consent/");
    await expect(page.getByText("同意済み")).toBeVisible();
    await page.getByRole("button", { name: "同意を撤回する" }).click();

    await expect.poll(() => putBody).toEqual({ contentScan: 0 });
  });
});

test.describe("consent unavailable state", () => {
  test("shows 準備中 and no rows when GET /me is 404", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await page.goto("/consent/");

    await expect(page.getByText("同意は準備中です")).toBeVisible();
    await expect(page.getByText("まだ同意していません")).toHaveCount(0);
  });
});
