import {
  expect,
  test,
  type Page,
  type Request,
  type Route,
} from "@playwright/test";

import type { PermissionStatus } from "../src/device/proximity/definitions";
import {
  describeProximityError,
  isForegroundResume,
  missingPermissions,
  pipelineAction,
  proximityPermissionsReady,
  shouldRefreshTokens,
} from "../src/device/proximity/pipeline";

// 近接パイプラインの起動/停止 (ProximityDriver) を検証する。ドライバは AppShell では
// なく src/app/layout.tsx から全ルートに 1 つマウントされるため、AppShell を使わない
// /compose/ や /group/new/ でも近接パイプラインが起動し続ける (Gap-1 の回帰防止)。
// 純関数はブラウザを開かずに、ライフサイクルは route mock + addInitScript で検証する。
// 位置権限は navigator.permissions.query を差し替えて決定的にする (web.ts の
// checkPermissions はこの API だけを見る)。

const API_BASE = "https://chain.i32.jp";
const SESSION_KEY = "chain.auth.session.v1";

const CORS_HEADERS: Record<string, string> = {
  "access-control-allow-origin": "*",
  "access-control-allow-methods": "GET,POST,PUT,OPTIONS",
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

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

/** JWT 風の文字列。session.ts は payload の iat / exp だけを読む。 */
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

async function stubLocationPermission(
  page: Page,
  state: "granted" | "prompt" | "denied",
): Promise<void> {
  await page.addInitScript((value) => {
    const status = {
      state: value,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    };
    Object.defineProperty(navigator, "permissions", {
      configurable: true,
      value: { query: () => Promise.resolve(status) },
    });
  }, state);
}

function permissions(overrides: Partial<PermissionStatus> = {}): PermissionStatus {
  return { bluetooth: "granted", location: "granted", notifications: "prompt", ...overrides };
}

test.describe("proximityPermissionsReady", () => {
  test("requires bluetooth and location but not notifications", () => {
    expect(proximityPermissionsReady(null)).toBe(false);
    expect(proximityPermissionsReady(permissions())).toBe(true);
    expect(proximityPermissionsReady(permissions({ notifications: "denied" }))).toBe(true);
    expect(proximityPermissionsReady(permissions({ location: "prompt" }))).toBe(false);
    expect(proximityPermissionsReady(permissions({ bluetooth: "denied" }))).toBe(false);
  });
});

test.describe("missingPermissions", () => {
  test("lists only required permissions that are not granted", () => {
    expect(missingPermissions(null)).toEqual(["bluetooth", "location"]);
    expect(missingPermissions(permissions())).toEqual([]);
    expect(missingPermissions(permissions({ location: "prompt" }))).toEqual(["location"]);
    expect(
      missingPermissions(permissions({ bluetooth: "denied", notifications: "denied" })),
    ).toEqual(["bluetooth"]);
  });
});

test.describe("pipelineAction", () => {
  test("starts only when signed in, permitted, and not already running", () => {
    expect(
      pipelineAction({ signedIn: true, running: false, permissionsReady: true }),
    ).toBe("start");
    expect(pipelineAction({ signedIn: true, running: true, permissionsReady: true })).toBe("none");
    expect(pipelineAction({ signedIn: true, running: false, permissionsReady: false })).toBe(
      "none",
    );
  });

  test("stops a running pipeline after sign-out", () => {
    expect(pipelineAction({ signedIn: false, running: true, permissionsReady: true })).toBe("stop");
    expect(pipelineAction({ signedIn: false, running: false, permissionsReady: true })).toBe(
      "none",
    );
  });
});

test.describe("isForegroundResume", () => {
  test("is true only on hidden -> visible", () => {
    expect(isForegroundResume(true, false)).toBe(true);
    expect(isForegroundResume(false, true)).toBe(false);
    expect(isForegroundResume(true, true)).toBe(false);
    expect(isForegroundResume(false, false)).toBe(false);
  });
});

test.describe("describeProximityError", () => {
  test("maps native error codes to messages and keeps null as null", () => {
    expect(describeProximityError(null)).toBeNull();
    const codes = [
      "permission_revoked",
      "bluetooth_off",
      "bluetooth_unsupported",
      "advertise_failed",
      "scan_failed",
      "tokens_exhausted",
    ] as const;
    for (const code of codes) {
      expect(describeProximityError(code)).toBeTruthy();
    }
  });
});

test.describe("shouldRefreshTokens", () => {
  const nowMs = Date.parse("2026-10-17T00:00:00Z");
  const hoursLater = (h: number) => new Date(nowMs + h * 3_600_000).toISOString();

  test("true when native has no tokens or reports tokens_exhausted", () => {
    expect(shouldRefreshTokens({ tokensValidUntil: null, lastError: null, nowMs })).toBe(true);
    expect(
      shouldRefreshTokens({
        tokensValidUntil: hoursLater(20),
        lastError: "tokens_exhausted",
        nowMs,
      }),
    ).toBe(true);
  });

  test("true under 6h remaining, false above", () => {
    expect(shouldRefreshTokens({ tokensValidUntil: hoursLater(5), lastError: null, nowMs })).toBe(
      true,
    );
    expect(shouldRefreshTokens({ tokensValidUntil: hoursLater(7), lastError: null, nowMs })).toBe(
      false,
    );
  });
});

test.describe("app-wide proximity driver", () => {
  test("signedIn + granted permissions starts the pipeline (running)", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await stubLocationPermission(page, "granted");
    const beaconCalls: string[] = [];

    await mockApi(page, async (route, request) => {
      const path = new URL(request.url()).pathname;
      if (path === "/api/v1/beacon/tokens") {
        beaconCalls.push(path);
        await fulfillJson(route, 200, {
          windowSec: 900,
          tokens: [
            {
              windowStart: new Date(Date.now() - 60_000).toISOString(),
              token: "opaque-token",
            },
          ],
        });
        return;
      }
      if (path === "/api/v1/encounters/sightings") {
        await fulfillJson(route, 200, { accepted: 0, dropped: 0 });
        return;
      }
      if (path === "/api/v1/me/location") {
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });

    await page.goto("/");

    const driver = page.getByTestId("proximity-driver");
    await expect(driver).toHaveAttribute("data-state", "running");
    expect(beaconCalls.length).toBeGreaterThan(0);
  });

  test("without location permission shows the manual request UI (blocked)", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await stubLocationPermission(page, "prompt");

    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });

    await page.goto("/");

    const driver = page.getByTestId("proximity-driver");
    await expect(driver).toHaveAttribute("data-state", "blocked");
    await expect(driver).toHaveAttribute("data-missing", "location");

    const button = driver.getByRole("button", { name: "許可する" });
    await expect(button).toBeVisible();

    // 固定表示の検証: バナーはタップでき (actionability check)、タブバーに重ならない。
    // trial click は実際には押さず、覆われていないことだけを検証する。
    await button.click({ trial: true });

    const bannerBox = await driver.getByRole("status").boundingBox();
    const viewport = page.viewportSize();
    expect(bannerBox).not.toBeNull();
    expect(viewport).not.toBeNull();
    if (bannerBox && viewport) {
      expect(bannerBox.y).toBeGreaterThanOrEqual(0);
      expect(bannerBox.y + bannerBox.height).toBeLessThanOrEqual(viewport.height + 1);
    }

    const tabBarBox = await page
      .getByRole("navigation", { name: "メインナビゲーション" })
      .boundingBox();
    expect(tabBarBox).not.toBeNull();
    if (bannerBox && tabBarBox) {
      expect(bannerBox.y + bannerBox.height).toBeLessThanOrEqual(tabBarBox.y + 1);
    }
  });

  test("signedOut does not mount the driver or fetch beacon tokens", async ({ page }) => {
    const beaconCalls: string[] = [];

    await mockApi(page, async (route, request) => {
      if (new URL(request.url()).pathname === "/api/v1/beacon/tokens") {
        beaconCalls.push(request.url());
      }
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });

    await page.goto("/");
    await page.waitForURL((url) => url.pathname.startsWith("/welcome"));

    await expect(page.getByTestId("proximity-driver")).toHaveCount(0);
    expect(beaconCalls).toHaveLength(0);
  });

  test("mounts the driver on routes that do not use AppShell", async ({ page }) => {
    await useStoredSession(page, makeStoredSession());
    await stubLocationPermission(page, "granted");

    await mockApi(page, async (route) => {
      await fulfillJson(route, 404, { error: { code: "not_found", message: "not found" } });
    });

    for (const path of ["/compose/", "/group/new/"]) {
      await page.goto(path);
      await expect(page.getByTestId("proximity-driver")).toBeAttached();
    }
  });
});
