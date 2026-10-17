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

const TEST_LAT = 35;
const TEST_LON = 135;

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

type AreaRow = {
  id: string;
  label: string;
  lat: number;
  lon: number;
  radiusM: number;
  createdAt: string;
};

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-11-chain-fe-be-gap-closure.txt",
);

test.use({
  geolocation: { latitude: TEST_LAT, longitude: TEST_LON },
  permissions: ["geolocation"],
});

type StoredSession = {
  did: string;
  accessToken: string;
  refreshToken: string;
  accessExpiresAtMs: number;
};

type ApiMock = (route: Route, request: Request) => Promise<void> | void;

type Captured = { method: string; path: string; body: unknown };

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

function areaSection(page: Page) {
  return page.locator("section", { hasText: "エリア" });
}

/** /me と /me/areas (一覧/作成/削除) を、状態を持つ in-memory リストでモックする。 */
async function mockAreasApi(
  page: Page,
  initial: AreaRow[],
  opts: { createStatus?: number; createMessage?: string } = {},
): Promise<{ rows: AreaRow[]; captured: Captured[] }> {
  const rows: AreaRow[] = [...initial];
  const captured: Captured[] = [];
  let seq = 0;

  await mockApi(page, async (route, request) => {
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();

    if (path === "/api/v1/me" && method === "GET") {
      await fulfillJson(route, 200, ME);
      return;
    }
    if (path === "/api/v1/me/areas" && method === "GET") {
      await fulfillJson(route, 200, { items: rows });
      return;
    }
    if (path === "/api/v1/me/areas" && method === "POST") {
      const body = request.postDataJSON() as {
        label: string;
        lat: number;
        lon: number;
        radiusM: number;
      };
      captured.push({ method, path, body });
      if (opts.createStatus && opts.createStatus >= 400) {
        await fulfillJson(route, opts.createStatus, {
          error: {
            code: "invalid_request",
            message: opts.createMessage ?? "invalid",
          },
        });
        return;
      }
      seq += 1;
      const row: AreaRow = {
        id: `a${seq}`,
        label: body.label,
        lat: body.lat,
        lon: body.lon,
        radiusM: body.radiusM,
        createdAt: "2026-10-17T00:00:00.000Z",
      };
      rows.push(row);
      await fulfillJson(route, 201, row);
      return;
    }
    if (path.startsWith("/api/v1/me/areas/") && method === "DELETE") {
      const id = decodeURIComponent(path.slice("/api/v1/me/areas/".length));
      captured.push({ method, path, body: null });
      const index = rows.findIndex((row) => row.id === id);
      if (index >= 0) {
        rows.splice(index, 1);
      }
      await route.fulfill({ status: 204, headers: CORS_HEADERS });
      return;
    }
    await fulfillJson(route, 404, {
      error: { code: "not_found", message: "not found" },
    });
  });

  return { rows, captured };
}

function makeArea(id: string, label: string): AreaRow {
  return { id, label, lat: TEST_LAT, lon: TEST_LON, radiusM: 500, createdAt: "2026-01-01T00:00:00.000Z" };
}

test.beforeEach(async ({ page }) => {
  await useStoredSession(page, makeStoredSession());
});

test.describe("settings areas", () => {
  test("creates two areas from the current location and lists them", async ({
    page,
  }, testInfo) => {
    const { rows, captured } = await mockAreasApi(page, []);
    await page.goto("/settings/");

    const section = areaSection(page);
    await expect(section.getByText("まだエリアはありません")).toBeVisible();

    const label = section.getByLabel("エリア名");
    const add = section.getByRole("button", { name: "追加" });

    await label.fill("図書館");
    await expect(add).toBeEnabled({ timeout: 15_000 });
    await add.click();
    await expect(section.getByText("図書館")).toBeVisible();

    await label.fill("職場");
    await expect(add).toBeEnabled({ timeout: 15_000 });
    await add.click();
    await expect(section.getByText("職場")).toBeVisible();

    expect(rows.map((row) => row.label)).toEqual(["図書館", "職場"]);
    const posts = captured.filter((item) => item.method === "POST");
    expect(posts).toHaveLength(2);
    expect(posts[0].body).toMatchObject({ label: "図書館", lat: TEST_LAT, lon: TEST_LON });
    expect(typeof (posts[0].body as { radiusM: number }).radiusM).toBe("number");

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({
        path: EVIDENCE_PATH.replace(/\.txt$/, ".png"),
        fullPage: true,
      });
    }
  });

  test("deletes an area via DELETE /me/areas/{id}", async ({ page }) => {
    const { rows, captured } = await mockAreasApi(page, [
      makeArea("a1", "図書館"),
      makeArea("a2", "職場"),
    ]);
    await page.goto("/settings/");

    const section = areaSection(page);
    await expect(section.getByText("図書館")).toBeVisible();

    await section.getByRole("button", { name: "エリア「図書館」を削除" }).click();

    await expect(section.getByText("図書館")).toHaveCount(0);
    await expect(section.getByText("職場")).toBeVisible();

    const deletes = captured.filter((item) => item.method === "DELETE");
    expect(deletes).toHaveLength(1);
    expect(deletes[0].path).toBe("/api/v1/me/areas/a1");
    expect(rows.map((row) => row.id)).toEqual(["a2"]);
  });

  test("disables creation at the 10-area limit", async ({ page }) => {
    const ten = Array.from({ length: 10 }, (_, index) =>
      makeArea(`a${index + 1}`, `エリア${index + 1}`),
    );
    await mockAreasApi(page, ten);
    await page.goto("/settings/");

    const section = areaSection(page);
    await expect(section.getByText("エリアは10個までです")).toBeVisible();
    await expect(section.getByRole("button", { name: "追加" })).toBeDisabled();
    await expect(section.getByLabel("エリア名")).toBeDisabled();
  });

  test("shows the backend validation error when create returns 400", async ({
    page,
  }) => {
    await mockAreasApi(page, [], {
      createStatus: 400,
      createMessage: "50 から 5000 の範囲で指定してください",
    });
    await page.goto("/settings/");

    const section = areaSection(page);
    const label = section.getByLabel("エリア名");
    const add = section.getByRole("button", { name: "追加" });

    await label.fill("大きすぎるエリア");
    await expect(add).toBeEnabled({ timeout: 15_000 });
    await add.click();

    await expect(
      section.getByText("50 から 5000 の範囲で指定してください"),
    ).toBeVisible();
  });
});
