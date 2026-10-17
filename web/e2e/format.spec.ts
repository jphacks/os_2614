import { expect, test } from "@playwright/test";

import { clockTime, elapsed, relativeTime } from "../src/data/format";

// 純関数のテスト。ブラウザもサーバーも使わないので決定的に速く回る。

test.describe("relativeTime", () => {
  test("returns the elapsed minutes for recent timestamps", () => {
    expect(relativeTime("2026-01-01T00:00:00Z", "2026-01-01T00:10:00Z")).toBe(
      "10分前",
    );
  });

  test("returns an empty string for an unparsable timestamp", () => {
    expect(relativeTime("not-a-date", "2026-01-01T00:00:00Z")).toBe("");
  });
});

test.describe("clockTime", () => {
  test("pads hours and minutes to two digits", () => {
    expect(clockTime(new Date(2026, 0, 1, 9, 12))).toBe("09:12");
  });

  test("returns an empty string for an unparsable timestamp", () => {
    expect(clockTime("not-a-date")).toBe("");
  });
});

test.describe("elapsed", () => {
  test("formats the difference as hh:mm:ss", () => {
    expect(elapsed(0, 3_723_000)).toBe("01:02:03");
  });

  test("clamps negative differences to zero", () => {
    expect(elapsed(1_000, 0)).toBe("00:00:00");
  });
});
