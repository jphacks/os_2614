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

const TEST_LAT = 35;
const TEST_LON = 135;

const FRIENDS = {
  items: [
    {
      did: "did:plc:alice",
      handle: "alice.bsky.social",
      displayName: "アリス",
      since: "2026-01-01T00:00:00.000Z",
      precision: "area",
    },
    {
      did: "did:plc:bob",
      handle: "bob.bsky.social",
      displayName: "ボブ",
      since: "2026-02-01T00:00:00.000Z",
      precision: "area",
    },
  ],
};

const CREATED_GROUP = {
  id: "g9",
  name: "情報科学演習 A",
  description: "",
  visibility: "open",
  memberCount: 1,
  isMember: true,
  state: "active",
  radiusM: 40,
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-18-chain-fe-be-gap-closure.png",
);

type StoredSession = {
  did: string;
  accessToken: string;
  refreshToken: string;
  accessExpiresAtMs: number;
};

type ApiMock = (route: Route, request: Request) => Promise<void> | void;

/** POST /groups の body (09 §2)。テストで形を検査する。 */
type GroupBody = {
  name: string;
  description: string;
  visibility: string;
  persistent: boolean;
  geofence: { lat: number; lon: number; radiusM: number };
  inviteQuorum: number;
  force: boolean;
};

test.use({
  geolocation: { latitude: TEST_LAT, longitude: TEST_LON },
  permissions: ["geolocation"],
});

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

/** JWT 風の文字列。session.ts は payload の iat / exp だけを読む。 */
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

/** chain.i32.jp への API 呼び出しをモックする。CORS プリフライトも同じ場所で返す。 */
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

/** 作成画面を開き、名前を入れて、位置の到着で「作成」が押せるまで待つ。 */
async function readyToSubmit(page: Page) {
  await page.goto("/group/new/");
  await page.getByLabel("グループ名").fill("情報科学演習 A");
  const submit = page.getByRole("button", { name: "作成", exact: true });
  await expect(submit).toBeEnabled({ timeout: 15_000 });
  return submit;
}

test.describe("group create", () => {
  test("409 の候補を提示し、force で再送して作成する", async ({
    page,
  }, testInfo) => {
    await useStoredSession(page, makeStoredSession());

    const bodies: GroupBody[] = [];
    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/friends") {
        await fulfillJson(route, 200, FRIENDS);
        return;
      }
      if (url.pathname === "/api/v1/groups" && request.method() === "POST") {
        bodies.push(request.postDataJSON() as GroupBody);
        if (bodies.length === 1) {
          await fulfillJson(route, 409, {
            error: {
              code: "conflict",
              message: "状態が競合しています",
              details: { similar: [{ id: "g1", name: "情報科学演習 旧" }] },
            },
          });
          return;
        }
        await fulfillJson(route, 201, CREATED_GROUP);
        return;
      }
      if (url.pathname === "/api/v1/groups/g9") {
        await fulfillJson(route, 200, CREATED_GROUP);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    const submit = await readyToSubmit(page);
    await submit.click();

    // 409 の details.similar を候補として表示する。
    await expect(page.getByText("情報科学演習 旧")).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH, fullPage: true });
    }

    await page.getByRole("button", { name: "それでも作成する" }).click();

    await page.waitForURL(/\/group\/\?id=g9/);
    expect(bodies).toHaveLength(2);
    expect(bodies[0].force).toBe(false);
    expect(bodies[1].force).toBe(true);
    expect(bodies[1].geofence).toEqual({
      lat: TEST_LAT,
      lon: TEST_LON,
      radiusM: 40,
    });
    expect(typeof bodies[1].inviteQuorum).toBe("number");
  });

  test("open グループを 201 で作成し、spec body を送る", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    let body: GroupBody | null = null;
    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/friends") {
        await fulfillJson(route, 200, FRIENDS);
        return;
      }
      if (url.pathname === "/api/v1/groups" && request.method() === "POST") {
        body = request.postDataJSON() as GroupBody;
        await fulfillJson(route, 201, CREATED_GROUP);
        return;
      }
      if (url.pathname === "/api/v1/groups/g9") {
        await fulfillJson(route, 200, CREATED_GROUP);
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    const submit = await readyToSubmit(page);
    await submit.click();

    await page.waitForURL(/\/group\/\?id=g9/);
    expect(body).not.toBeNull();
    const sent = body as unknown as GroupBody;
    // 現在地が geofence の中心、半径は 20〜2000m (09 §2)。
    expect(sent.geofence).toEqual({ lat: TEST_LAT, lon: TEST_LON, radiusM: 40 });
    expect(sent.visibility).toBe("open");
    expect(sent.force).toBe(false);
    expect(sent.persistent).toBe(false);
    // inviteQuorum は人数。open は 1 で作る。
    expect(sent.inviteQuorum).toBe(1);
  });

  test("位置が古いと location_stale を表示する", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());

    await mockApi(page, async (route, request) => {
      const url = new URL(request.url());
      if (url.pathname === "/api/v1/friends") {
        await fulfillJson(route, 200, FRIENDS);
        return;
      }
      if (url.pathname === "/api/v1/groups" && request.method() === "POST") {
        await fulfillJson(route, 403, {
          error: { code: "location_stale", message: "位置情報が古いか、ありません" },
        });
        return;
      }
      await fulfillJson(route, 404, {
        error: { code: "not_found", message: "not found" },
      });
    });

    const submit = await readyToSubmit(page);
    await submit.click();

    await expect(
      page.getByText("位置情報が古いため作成できませんでした。少し待ってからもう一度お試しください。"),
    ).toBeVisible();
  });
});
