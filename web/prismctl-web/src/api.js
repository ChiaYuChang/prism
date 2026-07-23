export class ApiError extends Error {
  constructor(message, status, requestId) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.requestId = requestId;
  }
}

export function createApi({ baseURL = "/api/v1", tokenStore = sessionStorage, timeoutMs = 15000, onUnauthorized = () => {} } = {}) {
  async function request(path, options = {}) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), timeoutMs);
    const requestId = crypto.randomUUID();
    const headers = new Headers(options.headers || {});
    headers.set("Accept", "application/json");
    headers.set("X-Request-Id", requestId);
    const token = tokenStore.getItem("prismctl.adminToken");
    if (token) headers.set("X-PRISM-TOKEN", token);
    let body = options.body;
    if (body !== undefined && body !== null && !(body instanceof FormData)) {
      headers.set("Content-Type", "application/json");
      body = JSON.stringify(body);
    }
    try {
      const response = await fetch(`${baseURL.replace(/\/$/, "")}${path}`, { ...options, body, headers, signal: controller.signal });
      const text = await response.text();
      let payload = null;
      if (text) {
        try { payload = JSON.parse(text); } catch { payload = text; }
      }
      if (!response.ok) {
        if (response.status === 401) onUnauthorized();
        const message = typeof payload === "object" && payload?.error ? payload.error : response.statusText || "request failed";
        throw new ApiError(String(message).slice(0, 300), response.status, requestId);
      }
      return payload;
    } catch (error) {
      if (error.name === "AbortError") throw new ApiError("request timed out", 0, requestId);
      throw error;
    } finally {
      clearTimeout(timeout);
    }
  }
  return {
    request,
    whoami: () => request("/admin/whoami"),
    diagnostics: () => request("/admin/diagnostics?failure_limit=10"),
    listTokens: () => request("/admin/tokens?limit=500&next=1"),
    createToken: (body) => request("/admin/tokens", { method: "POST", body }),
    revokeToken: (id) => request(`/admin/tokens/${encodeURIComponent(id)}/revoke`, { method: "POST" }),
  };
}
