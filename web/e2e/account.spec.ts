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

/** GET /me が返す meResponse (backend/internal/controller/me.go)。 */
const ME = {
  did: "did:plc:testuser",
  handle: "test.bsky.social",
  displayName: "てすとたろう",
  settings: {
    encounterMode: "delayed",
    encounterDelaySec: 3600,
    friendBeaconShare: true,
    partyAuto: true,
    locationPaused: false,
    warnDisplay: "blur",
    notify: {},
  },
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-12-chain-fe-be-gap-closure.png",
);

type StoredSession = {
  did: string;
  accessToken: string;
  refreshToken: string;
  accessExpiresAtMs: number;
};

type ApiMock = (route: Route, request: Request) => Promise<void> | void;

type CapturedDelete = { path: string; deletePdsRecords: string | null };

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

function accountSection(page: Page) {
  return page.locator("section", { hasText: "アカウント" });
}

/** /me と DELETE /me をモックする。deletes には受け取った DELETE を積む。 */
async function mockAccountApi(
  page: Page,
  opts: { deleteStatus?: number } = {},
): Promise<CapturedDelete[]> {
  const deletes: CapturedDelete[] = [];
  await mockApi(page, async (route, request) => {
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();

    if (path === "/api/v1/me" && method === "GET") {
      await fulfillJson(route, 200, ME);
      return;
    }
    if (path === "/api/v1/me" && method === "DELETE") {
      deletes.push({
        path,
        deletePdsRecords: url.searchParams.get("deletePdsRecords"),
      });
      if (opts.deleteStatus && opts.deleteStatus >= 400) {
        await fulfillJson(route, opts.deleteStatus, {
          error: { code: "internal", message: "boom" },
        });
        return;
      }
      await route.fulfill({ status: 204, headers: CORS_HEADERS });
      return;
    }
    if (path === "/api/v1/auth/logout" && method === "POST") {
      await route.fulfill({ status: 204, headers: CORS_HEADERS });
      return;
    }
    await fulfillJson(route, 404, {
      error: { code: "not_found", message: "not found" },
    });
  });
  return deletes;
}

// ログアウトで画面遷移するため、Playwright 1.64.1 の fullyParallel 時の
// trace 後始末 ENOENT (settings.spec.ts と同じ) を避けてこの spec は trace を切る。
test.use({ trace: "off" });

test.beforeEach(async ({ page }) => {
  await useStoredSession(page, makeStoredSession());
});

test.describe("account deletion", () => {
  test("sends DELETE /me and clears the session, then lands on /welcome/", async ({
    page,
  }, testInfo) => {
    const deletes = await mockAccountApi(page);
    await page.goto("/settings/");

    const section = accountSection(page);
    const confirm = section.locator("#delete-confirm");
    const remove = section.getByRole("button", { name: "アカウントを削除" });

    // 確認語を入れるまで押せない。
    await expect(remove).toBeDisabled();

    await section.getByRole("switch", { name: "Bluesky 上の投稿も削除" }).click();
    await confirm.fill("削除");
    await expect(remove).toBeEnabled();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH, fullPage: true });
    }

    await remove.click();

    await page.waitForURL((url) => url.pathname.startsWith("/welcome"), {
      timeout: 15_000,
    });

    expect(deletes).toEqual([{ path: "/api/v1/me", deletePdsRecords: "true" }]);
    const stored = await page.evaluate(
      (key) => window.localStorage.getItem(key),
      SESSION_KEY,
    );
    expect(stored).toBeNull();
  });

  test("keeps the session and shows the error when DELETE /me fails", async ({
    page,
  }) => {
    await mockAccountApi(page, { deleteStatus: 502 });
    await page.goto("/settings/");

    const section = accountSection(page);
    await section.locator("#delete-confirm").fill("削除");
    await section.getByRole("button", { name: "アカウントを削除" }).click();

    await expect(section.getByText("boom")).toBeVisible();
    await page.waitForTimeout(300);
    expect(new URL(page.url()).pathname).toBe("/settings/");
    const stored = await page.evaluate(
      (key) => window.localStorage.getItem(key),
      SESSION_KEY,
    );
    expect(stored).not.toBeNull();
  });
});
