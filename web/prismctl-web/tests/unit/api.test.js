import { describe, expect, it, vi } from "vitest";
import { ApiError, createApi } from "../../src/api.js";

describe("API client", () => {
  it("sends the session token and request ID", async () => {
    const storage = new Map([["prismctl.adminToken", "padm_secret"]]);
    vi.stubGlobal("sessionStorage", { getItem: (key) => storage.get(key), setItem: vi.fn() });
    vi.stubGlobal("crypto", { randomUUID: () => "request-1" });
    vi.stubGlobal("fetch", vi.fn(async (_url, init) => new Response(JSON.stringify({ ok: true }), { status: 200, headers: { "content-type": "application/json" }, ...init })));
    const api = createApi({ tokenStore: sessionStorage });
    await api.request("/admin/whoami");
    expect(fetch.mock.calls[0][1].headers.get("X-PRISM-TOKEN")).toBe("padm_secret");
    expect(fetch.mock.calls[0][1].headers.get("X-Request-Id")).toBe("request-1");
  });

  it("clears authorization on 401 and preserves status", async () => {
    const onUnauthorized = vi.fn();
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: "Unauthorized" }), { status: 401 })));
    const api = createApi({ tokenStore: { getItem: () => "token" }, onUnauthorized });
    await expect(api.request("/admin/whoami")).rejects.toBeInstanceOf(ApiError);
    expect(onUnauthorized).toHaveBeenCalledOnce();
  });

  it("generates a request ID when randomUUID is unavailable", async () => {
    vi.stubGlobal("crypto", { getRandomValues: (bytes) => bytes.fill(171) });
    vi.stubGlobal("fetch", vi.fn(async (_url, init) => new Response(JSON.stringify({ ok: true }), { status: 200, ...init })));
    const api = createApi({ tokenStore: { getItem: () => "token" } });
    await api.request("/admin/whoami");
    expect(fetch.mock.calls[0][1].headers.get("X-Request-Id")).toBe("ab".repeat(16));
  });
});
