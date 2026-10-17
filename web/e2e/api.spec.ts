import { expect, test } from "@playwright/test";

import { ApiError } from "../src/auth/errors";
import { fetchAllPages, fetchPage, toResult } from "../src/data/api";

// 純関数テスト。ブラウザは開かず、global fetch と localStorage だけをスタブして
// 実モジュール (src/data/api.ts → src/auth/api.ts → src/auth/session.ts) を通す。

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

test.describe("fetchPage / fetchAllPages", () => {
  test("follows the cursor across two pages and concatenates them", async () => {
    const calls: string[] = [];
    handler = (url) => {
      calls.push(url);
      const cursor = new URL(url).searchParams.get("cursor");
      if (cursor === null) {
        // 1 ページ目: 次ページの cursor を返す。
        return json(200, { items: [{ id: 1 }, { id: 2 }], cursor: "page2" });
      }
      // 2 ページ目: cursor を省略して終端を示す。
      return json(200, { items: [{ id: 3 }] });
    };

    const first = await fetchPage<{ id: number }>("/items", { limit: 2 });
    expect(first.items.map((item) => item.id)).toEqual([1, 2]);
    expect(first.cursor).toBe("page2");
    // limit は api() の query 経由で URL に載る (API_BASE は api() が組む)。
    expect(calls[0]).toContain("limit=2");

    const all = await fetchAllPages<{ id: number }>("/items", { limit: 2 });
    expect(all.map((item) => item.id)).toEqual([1, 2, 3]);
    // 2 回目の fetch は 1 ページ目の cursor を渡している。
    expect(calls[1]).toContain("limit=2");
    expect(calls[1]).not.toContain("cursor=");
    expect(calls[2]).toContain("cursor=page2");
  });

  test("returns cursor: undefined when the backend omits it", async () => {
    handler = () => json(200, { items: [1, 2] });

    const page = await fetchPage<number>("/items");

    expect(page.items).toEqual([1, 2]);
    expect(page.cursor).toBeUndefined();
    expect("cursor" in page).toBe(true);
  });
});

test.describe("toResult", () => {
  test("classifies 404 as unavailable", async () => {
    handler = () => json(404, { error: { code: "not_found", message: "not found" } });

    const result = await toResult(() => fetchPage("/items"));

    expect(result.status).toBe("unavailable");
    if (result.status === "unavailable") {
      expect(result.error).toBeInstanceOf(ApiError);
      expect(result.error.status).toBe(404);
      expect(result.error.code).toBe("not_found");
    }
  });

  test("classifies 500 as error", async () => {
    handler = () => json(500, { error: { code: "internal", message: "boom" } });

    const result = await toResult(() => fetchPage("/items"));

    expect(result.status).toBe("error");
    if (result.status === "error") {
      expect(result.error).toBeInstanceOf(ApiError);
      expect(result.error.status).toBe(500);
    }
  });

  test("returns ok with the decoded data on success", async () => {
    handler = () => json(200, { items: [{ id: 1 }] });

    const result = await toResult(() => fetchPage<{ id: number }>("/items"));

    expect(result.status).toBe("ok");
    if (result.status === "ok") {
      expect(result.data.items).toEqual([{ id: 1 }]);
    }
  });
});
