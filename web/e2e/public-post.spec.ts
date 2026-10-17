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

type PostBody = { spaceIds: string[]; text: string; alsoPublic: boolean };

const ME = {
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
    contentScanConsentVersion: 1,
    notify: {},
  },
};

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
  "../.omo/evidence/chain-fe-be-gap-closure/task-36-chain-fe-be-gap-closure.png",
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

/** /compose/ を開き、送信先の読み込みを待って本文を入れる。 */
async function readyToPost(page: Page, text: string) {
  await page.goto("/compose/");
  const textarea = page.getByLabel("投稿本文");
  await expect(textarea).toBeVisible({ timeout: 15_000 });
  await textarea.fill(text);
}

test.describe("public post option", () => {
  test("shows public.error while keeping the Space post successful", async ({
    page,
  }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    let body: PostBody | null = null;
    await mockApi(page, async (route, request) => {
      const pathname = apiPath(request.url());
      if (pathname === "/api/v1/instances") {
        await fulfillJson(route, 200, { items: [GROUP_INSTANCE] });
        return;
      }
      if (pathname === "/api/v1/me") {
        await fulfillJson(route, 200, ME);
        return;
      }
      if (pathname === "/api/v1/posts" && request.method() === "POST") {
        body = request.postDataJSON() as PostBody;
        await fulfillJson(route, 201, {
          items: [{ uri: "at://did:plc:testuser/app.bsky.feed.post/1", spaceId: "g1" }],
          public: { error: "upstream" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    await readyToPost(page, "公開投稿つき");
    await page.getByRole("button", { name: "公開", exact: true }).click();
    await page.getByRole("button", { name: "投稿する" }).click();

    await expect(
      page.getByText(
        "公開投稿は作成できませんでした。Space への投稿は完了しています。",
      ),
    ).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH });
    }

    // instance 投稿は成功しているので、公開投稿の失敗で画面遷移はしない。
    await expect(page).toHaveURL(/\/compose\/?$/);
    expect(body).not.toBeNull();
    expect((body as unknown as PostBody).alsoPublic).toBe(true);
  });

  test("navigates away when the public post succeeds", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const pathname = apiPath(request.url());
      if (pathname === "/api/v1/instances") {
        await fulfillJson(route, 200, { items: [GROUP_INSTANCE] });
        return;
      }
      if (pathname === "/api/v1/me") {
        await fulfillJson(route, 200, ME);
        return;
      }
      if (pathname === "/api/v1/posts" && request.method() === "POST") {
        await fulfillJson(route, 201, {
          items: [{ uri: "at://did:plc:testuser/app.bsky.feed.post/1", spaceId: "g1" }],
          public: { uri: "at://did:plc:testuser/app.bsky.feed.post/1", cid: "bafy" },
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

    await readyToPost(page, "公開投稿つき");
    await page.getByRole("button", { name: "公開", exact: true }).click();
    await page.getByRole("button", { name: "投稿する" }).click();

    await expect(page).toHaveURL(/\/group\/\?id=g1$/);
  });
});
