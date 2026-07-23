import { describe, expect, it } from "vitest";
import { parsePermission, permissionHex } from "../../src/validation.js";

describe("permission masks", () => {
  it("accepts decimal and hexadecimal values", () => {
    expect(parsePermission("97")).toBe(97);
    expect(parsePermission("0x61")).toBe(97);
    expect(permissionHex(97)).toBe("0x61");
  });

  it("rejects out of range and reserved masks", () => {
    expect(() => parsePermission("256")).toThrow();
    expect(() => parsePermission("0x02")).toThrow();
    expect(() => parsePermission("0x81")).toThrow();
  });
});
