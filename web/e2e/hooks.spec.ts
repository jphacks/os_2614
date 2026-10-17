import { expect, test } from "@playwright/test";

import { ApiError } from "../src/auth/errors";
import { fetchPage, toResult } from "../src/data/api";
import { deriveState } from "../src/data/hooks";

// hooks.ts の状態導出を試す純関数テスト。React フックそのものはテストランナが
// 無いので、フックが内部で使う純関数 deriveState を直接検証する。
// 後半は実モジュール (fetchPage → auth/api → auth/session) を通し、toResult の
// 分類結果が deriveState で 4 状態に落ちることをエンドツーエンドで確かめる。

const SESSION_KEY = "chain.auth.session.v1";

/** session.ts が「期限内」とみなす保存値を localStorage に置く。 */
function installSession(): void {
  const map = new Map<string, string>();
  const storage: Storage = {
    get length() {
      return map.size;
    },
    clear: () => map.clear(),
    getItem: (key) => map.get(key) ?? null,
    key: (index) => Array.from(map.keys())[index] ?? null,
    removeItem: (key) => void map.delete(key),
    setItem: (key, value) => void map.set(key, value),
  };
  (globalThis as { localStorage: Storage }).localStorage = storage;
  // session.ts は保存値の変更を window の storage イベントで監視する。Node には無いので空で足す。
  (globalThis as { window: Window }).window = {
    addEventListener: () => {},
    removeEventListener: () => {},
  } as unknown as Window;
  storage.setItem(
    SESSION_KEY,
    JSON.stringify({
      did: "did:plc:testuser",
      accessToken: "test-access-token",
      refreshToken: "test-refresh-token",
      accessExpiresAtMs: Date.now() + 60 * 60 * 1000,
    }),
  );
}

installSession();

type FetchHandler = (url: string, init?: RequestInit) => Response | Promise<Response>;

let handler: FetchHandler = () => json(500, { error: { code: "internal", message: "no handler" } });

globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
  const url =
    typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
  return Promise.resolve(handler(url, init));
}) as typeof fetch;

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

const isEmptyPage = (page: { items: unknown[] }): boolean => page.items.length === 0;

test.describe("deriveState (pure)", () => {
  test("returns ok with the data when the result is ok and not empty", () => {
    const state = deriveState({ status: "ok", data: { name: "alice" } }, () => false);

    expect(state.status).toBe("ok");
    if (state.status === "ok") {
      expect(state.data).toEqual({ name: "alice" });
    }
  });

  test("collapses ok to empty when the predicate matches", () => {
    const state = deriveState({ status: "ok", data: [] as number[] }, (items) => items.length === 0);

    expect(state.status).toBe("empty");
    expect("data" in state).toBe(false);
  });

  test("keeps ok even for an empty payload when no predicate is given", () => {
    const state = deriveState({ status: "ok", data: [] as number[] });

    expect(state.status).toBe("ok");
    if (state.status === "ok") {
      expect(state.data).toEqual([]);
    }
  });

  test("keeps the ApiError for error", () => {
    const error = new ApiError(500, "internal", "boom");
    const state = deriveState({ status: "error", error });

    expect(state.status).toBe("error");
    if (state.status === "error") {
      expect(state.error).toBe(error);
    }
  });

  test("keeps the ApiError for unavailable", () => {
    const error = new ApiError(404, "not_found", "not found");
    const state = deriveState({ status: "unavailable", error });

    expect(state.status).toBe("unavailable");
    if (state.status === "unavailable") {
      expect(state.error).toBe(error);
      expect(state.error.status).toBe(404);
    }
  });
});

test.describe("deriveState over toResult + fetchPage", () => {
  test("success (ok): 200 with items", async () => {
    handler = () => json(200, { items: [{ id: 1 }, { id: 2 }], cursor: "next" });

    const result = await toResult(() => fetchPage<{ id: number }>("/items"));
    const state = deriveState(result, isEmptyPage);

    expect(state.status).toBe("ok");
    if (state.status === "ok") {
      expect(state.data.items.map((item) => item.id)).toEqual([1, 2]);
    }
  });

  test("empty: 200 with an empty items array", async () => {
    handler = () => json(200, { items: [] });

    const result = await toResult(() => fetchPage<{ id: number }>("/items"));
    const state = deriveState(result, isEmptyPage);

    expect(state.status).toBe("empty");
  });

  test("error: 500", async () => {
    handler = () => json(500, { error: { code: "internal", message: "boom" } });

    const result = await toResult(() => fetchPage<{ id: number }>("/items"));
    const state = deriveState(result, isEmptyPage);

    expect(state.status).toBe("error");
    if (state.status === "error") {
      expect(state.error).toBeInstanceOf(ApiError);
      expect(state.error.status).toBe(500);
    }
  });

  test("unavailable: 404", async () => {
    handler = () => json(404, { error: { code: "not_found", message: "not found" } });

    const result = await toResult(() => fetchPage<{ id: number }>("/items"));
    const state = deriveState(result, isEmptyPage);

    expect(state.status).toBe("unavailable");
    if (state.status === "unavailable") {
      expect(state.error).toBeInstanceOf(ApiError);
      expect(state.error.status).toBe(404);
    }
  });

  test("unavailable: 501 is not treated as error", async () => {
    handler = () => json(501, { error: { code: "not_implemented", message: "todo" } });

    const result = await toResult(() => fetchPage<{ id: number }>("/items"));
    const state = deriveState(result, isEmptyPage);

    expect(state.status).toBe("unavailable");
  });
});
