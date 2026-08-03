import { describe, expect, it } from "vitest";
import { formatAge } from "../../src/format.js";

describe("relative timestamps", () => {
  it("formats the age of a health response", () => {
    const now = Date.parse("2026-01-01T00:00:10Z");
    expect(formatAge("2026-01-01T00:00:00Z", now)).toBe("10s ago");
  });

  it("handles missing and invalid timestamps", () => {
    expect(formatAge()).toBe("never");
    expect(formatAge("not-a-timestamp")).toBe("unknown");
  });
});
