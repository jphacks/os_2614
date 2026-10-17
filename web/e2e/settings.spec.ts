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

/** GET /me が返す meResponse (backend/internal/controller/me.go)。 */
const ME = {
  did: "did:plc:testuser",
  handle: "test.bsky.social",
  displayName: "てすとたろう",
  settings: {
    encounterMode: "delayed",
    encounterDelaySec: 3600,
    friendBeaconShare: true,
    partyAuto: false,
    locationPaused: false,
    warnDisplay: "blur",
    notify: { "encounter.daily": false },
  },
};

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

/** JWT 風の文字列。session.ts は payload の iat / exp だけを読む。 */
function fakeJwt(claims: Record<string, unknown>): string {
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const payload = base64url(JSON.stringify(claims));
  return `${header}.${payload}.signature`;
}

function makeStoredSession(expiresInMs = 60 * 60 * 1000): StoredSession {
  const did = "did:plc:testuser";
  const now = Math.floor(Date.now() / 1000);
  const accessToken = fakeJwt({ sub: did, iat: now, exp: now + 900 });
  return {
    did,
    accessToken,
    refreshToken: "refresh-token-0",
    accessExpiresAtMs: Date.now() + expiresInMs,
  };
}

async function useStoredSession(
  page: Page,
  session: StoredSession,
): Promise<void> {
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

/** GET /me を指定の応答で返し、それ以外は 404 にする。 */
async function mockMe(page: Page, status = 200, body: unknown = ME): Promise<void> {
  await mockApi(page, async (route, request) => {
    if (new URL(request.url()).pathname === "/api/v1/me") {
      await fulfillJson(route, status, body);
      return;
    }
    await fulfillJson(route, 404, {
      error: { code: "not_found", message: "not found" },
    });
  });
}

const EVIDENCE_PATH = path.resolve(
  process.cwd(),
  "../.omo/evidence/chain-fe-be-gap-closure/task-9-chain-fe-be-gap-closure.png",
);

// Playwright 1.64.1 では fullyParallel 時に trace ファイルの後始末が
// browserContext.close で ENOENT になることがあるため、この spec は trace を切る。
test.use({ trace: "off" });

test.describe("settings reveal delay", () => {
  test.beforeEach(async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
  });

  test("shows 3 modes and only the 4 backend presets", async ({ page }) => {
    await mockMe(page);
    await page.goto("/settings/");

    const reveal = page.locator("section", { hasText: "すれ違いの公開" });
    const modes = reveal.getByRole("group", { name: "相手に見えるまで" });

    await expect(modes.getByRole("switch")).toHaveCount(3);
    await expect(modes.getByRole("switch", { name: "すぐ" })).toBeVisible();
    await expect(modes.getByRole("switch", { name: "一定時間後" })).toBeVisible();
    await expect(modes.getByRole("switch", { name: "非公開" })).toBeVisible();

    const presets = reveal.getByRole("group", { name: "公開までの時間" });
    await expect(presets.getByRole("radio")).toHaveCount(4);
    await expect(presets.getByRole("radio", { name: "1時間" })).toBeVisible();
    await expect(presets.getByRole("radio", { name: "6時間" })).toBeVisible();
    await expect(presets.getByRole("radio", { name: "1日" })).toBeVisible();
    await expect(presets.getByRole("radio", { name: "3日" })).toBeVisible();

    await expect(presets.getByRole("radio", { name: "3時間" })).toHaveCount(0);
    await expect(presets.getByRole("radio", { name: "12時間" })).toHaveCount(0);
  });

  test("choosing 非公開 selects private and hides the presets", async ({
    page,
  }) => {
    await mockMe(page);
    await page.goto("/settings/");

    const reveal = page.locator("section", { hasText: "すれ違いの公開" });
    const modes = reveal.getByRole("group", { name: "相手に見えるまで" });

    await modes.getByRole("switch", { name: "非公開" }).click();

    await expect(
      modes.getByRole("switch", { name: "非公開" }),
    ).toHaveAttribute("aria-checked", "true");
    await expect(modes.getByRole("switch", { name: "すぐ" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
    await expect(
      modes.getByRole("switch", { name: "一定時間後" }),
    ).toHaveAttribute("aria-checked", "false");
    await expect(reveal.getByRole("status")).toHaveText("非公開");
    await expect(
      reveal.getByRole("group", { name: "公開までの時間" }),
    ).toHaveCount(0);
  });

  test("keyboard selection works and presets update the value", async ({
    page,
  }) => {
    await mockMe(page);
    await page.goto("/settings/");

    const reveal = page.locator("section", { hasText: "すれ違いの公開" });
    const modes = reveal.getByRole("group", { name: "相手に見えるまで" });

    const immediate = modes.getByRole("switch", { name: "すぐ" });
    await immediate.focus();
    await page.keyboard.press("Enter");
    await expect(immediate).toHaveAttribute("aria-checked", "true");
    await expect(reveal.getByRole("status")).toHaveText("すぐ");
    await expect(
      reveal.getByRole("group", { name: "公開までの時間" }),
    ).toHaveCount(0);

    await modes.getByRole("switch", { name: "一定時間後" }).click();
    const presets = reveal.getByRole("group", { name: "公開までの時間" });
    await expect(presets.getByRole("radio")).toHaveCount(4);
    await presets.getByRole("radio", { name: "3日" }).click();
    await expect(presets.getByRole("radio", { name: "3日" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    await expect(reveal.getByRole("status")).toHaveText("3日");
  });
});

test.describe("settings backend model", () => {
  test.beforeEach(async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
  });

  test("renders the new toggles and warning control from GET /me", async (
    { page },
    testInfo,
  ) => {
    await mockMe(page);
    await page.goto("/settings/");

    await expect(
      page.getByRole("switch", { name: "フレンドへのビーコン共有" }),
    ).toHaveAttribute("aria-checked", "true");
    await expect(
      page.getByRole("switch", { name: "パーティに自動参加" }),
    ).toHaveAttribute("aria-checked", "false");
    await expect(
      page.getByRole("switch", { name: "位置共有の一時停止" }),
    ).toHaveAttribute("aria-checked", "false");

    const warn = page.locator("section", { hasText: "警告の表示方法" });
    await expect(warn.getByRole("radio", { name: "ぼかす" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    await expect(warn.getByRole("radio", { name: "そのまま表示" })).toHaveAttribute(
      "aria-checked",
      "false",
    );

    const notify = page.locator("section", { hasText: "通知の種類" });
    await expect(
      notify.getByRole("switch", { name: "フレンド申請" }),
    ).toHaveAttribute("aria-checked", "true");
    await expect(
      notify.getByRole("switch", { name: "すれ違い（毎日）" }),
    ).toHaveAttribute("aria-checked", "false");
    const locked = notify.getByRole("switch", { name: "投稿の非表示" });
    await expect(locked).toHaveAttribute("aria-checked", "true");
    await expect(locked).toBeDisabled();

    await expect(
      page.getByRole("button", { name: "ログアウト", exact: true }),
    ).toBeVisible();

    if (testInfo.project.name === "desktop-chrome") {
      mkdirSync(path.dirname(EVIDENCE_PATH), { recursive: true });
      await page.screenshot({ path: EVIDENCE_PATH, fullPage: true });
    }
  });

  test("toggling the switches updates the local state", async ({ page }) => {
    await mockMe(page);
    await page.goto("/settings/");

    const beacon = page.getByRole("switch", { name: "フレンドへのビーコン共有" });
    await beacon.click();
    await expect(beacon).toHaveAttribute("aria-checked", "false");

    const party = page.getByRole("switch", { name: "パーティに自動参加" });
    await party.click();
    await expect(party).toHaveAttribute("aria-checked", "true");

    const notifyDaily = page
      .locator("section", { hasText: "通知の種類" })
      .getByRole("switch", { name: "すれ違い（毎日）" });
    await notifyDaily.click();
    await expect(notifyDaily).toHaveAttribute("aria-checked", "true");
  });

  test("warning display uses the 3 backend presets", async ({ page }) => {
    await mockMe(page);
    await page.goto("/settings/");

    const warn = page.locator("section", { hasText: "警告の表示方法" });
    await expect(warn.getByRole("radio")).toHaveCount(3);
    await warn.getByRole("radio", { name: "隠す" }).click();
    await expect(warn.getByRole("radio", { name: "隠す" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    await expect(warn.getByRole("radio", { name: "ぼかす" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });

  test("shows the error state with a retry when GET /me fails", async ({
    page,
  }) => {
    await mockMe(page, 500, { error: { code: "internal", message: "boom" } });
    await page.goto("/settings/");

    await expect(page.getByText("設定を読み込めませんでした")).toBeVisible();
    await expect(page.getByRole("button", { name: "再読み込み" })).toBeVisible();
    await expect(
      page.getByRole("button", { name: "ログアウト", exact: true }),
    ).toBeVisible();
  });

  test("shows the unavailable state when GET /me is 404", async ({ page }) => {
    await mockMe(page, 404, { error: { code: "not_found", message: "not found" } });
    await page.goto("/settings/");

    await expect(page.getByText("設定は準備中です")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "ログアウト", exact: true }),
    ).toBeVisible();
  });
});
