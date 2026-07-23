import { createApi } from "./api.js";
import { clearSession, readToken, saveToken } from "./auth.js";
import { formatDate, pretty } from "./format.js";
import { parsePermission, permissionHex } from "./validation.js";

export function controlRoom() {
  const api = createApi({ onUnauthorized: () => clearSession() });
  return {
    authenticated: Boolean(readToken()),
    busy: false,
    error: "",
    tokenInput: "",
    section: "overview",
    operator: {},
    diagnostics: {},
    services: {},
    tokens: [],
    failedTasks: 0,
    showCreate: false,
    oneTimeSecret: "",
    createForm: { type: "user", name: "", permissions: "" },
    get canTokenAdmin() { return (Number(this.operator.permissions) & 0x40) === 0x40; },
    permissionHex,
    formatDate,
    pretty,
    async init() {
      if (this.authenticated) await this.refresh();
    },
    async login() {
      this.busy = true; this.error = "";
      try {
        saveToken(this.tokenInput);
        this.tokenInput = "";
        await this.refresh();
        this.authenticated = true;
      } catch (error) {
        clearSession(); this.authenticated = false; this.error = error.message;
      } finally { this.busy = false; }
    },
    logout() {
      clearSession(); this.authenticated = false; this.operator = {}; this.tokens = []; this.diagnostics = {}; this.oneTimeSecret = "";
    },
    async refresh() {
      this.busy = true; this.error = "";
      try {
        this.operator = await api.whoami();
        const [diagnostics, tokenResponse] = await Promise.all([api.diagnostics(), api.listTokens()]);
        this.diagnostics = diagnostics || {};
        this.services = this.diagnostics.services || {};
        this.failedTasks = (this.diagnostics.recent_failures || []).length;
        this.tokens = tokenResponse?.items || [];
      } catch (error) {
        if (error.status === 401) this.logout();
        else this.error = error.message;
      } finally { this.busy = false; }
    },
    async createToken() {
      this.busy = true; this.error = "";
      try {
        const permissions = parsePermission(this.createForm.permissions);
        const result = await api.createToken({ type: this.createForm.type, name: this.createForm.name, ...(permissions === undefined ? {} : { permissions }) });
        this.oneTimeSecret = result.token;
        this.createForm.name = "";
        await this.refresh();
      } catch (error) { this.error = error.message; } finally { this.busy = false; }
    },
    async revoke(item) {
      if (!window.confirm(`Revoke token ${item.name || item.id}?`)) return;
      this.busy = true; this.error = "";
      try { await api.revokeToken(item.id); await this.refresh(); } catch (error) { this.error = error.message; } finally { this.busy = false; }
    },
  };
}
