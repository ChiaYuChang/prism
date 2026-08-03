import { createApi } from "./api.js";
import { clearSession, readToken, saveToken } from "./auth.js";
import { formatAge, formatDate, pretty } from "./format.js";
import { parsePermission, permissionHex } from "./validation.js";

export function controlRoom() {
  const api = createApi({ onUnauthorized: () => clearSession() });
  return {
    authenticated: Boolean(readToken()),
    route: normalizeRoute(window.location.pathname),
    busy: false,
    error: "",
    tokenInput: "",
    operator: {},
    diagnostics: {},
    services: {},
    serviceMetadata: {},
    serviceGroups: [],
    ageTick: 0,
    servicesExpanded: false,
    tokens: [],
    failedTasks: 0,
    failedTaskDetails: [],
    failedTasksExpanded: false,
    showCreate: false,
    oneTimeSecret: "",
    createForm: { type: "user", name: "", permissions: "" },
    get canTokenAdmin() { return (Number(this.operator.permissions) & 0x40) === 0x40; },
    get showLogin() { return this.route === "/auth" && !this.authenticated; },
    get showApp() { return this.authenticated && this.route !== "/auth"; },
    get loginDisabled() { return this.busy || !this.tokenInput; },
    get issueDisabled() { return this.busy || !this.canTokenAdmin; },
    get operatorLabel() { return this.operator.name || this.operator.token_id || ""; },
    get operatorPermissionHex() { return this.permissionHex(this.operator.permissions); },
    get operatorSummary() { return `${this.operator.type || ""} / ${this.operatorPermissionHex}`; },
    get heading() {
      if (this.route === "/tokens") return "Token control";
      if (this.route === "/diagnostics") return "Diagnostics";
      return "System overview";
    },
    get isOverview() { return this.route === "/overview"; },
    get isDiagnostics() { return this.route === "/diagnostics"; },
    get isTokens() { return this.route === "/tokens"; },
    get serviceCount() { return Object.keys(this.services).length; },
    get noServices() { return this.serviceCount === 0; },
    get diagnosticsStatus() { return this.diagnostics.nats_error || "healthy response"; },
    get diagnosticsText() { return this.pretty(this.diagnostics); },
    setTokenInput(event) { this.tokenInput = event.target.value; },
    setCreateName(event) { this.createForm.name = event.target.value; },
    setCreateType(event) { this.createForm.type = event.target.value; },
    setCreatePermissions(event) { this.createForm.permissions = event.target.value; },
    toggleServices() { this.servicesExpanded = !this.servicesExpanded; },
    toggleFailedTasks() { this.failedTasksExpanded = !this.failedTasksExpanded; },
    permissionHex,
    formatDate,
    pretty,
    async init() {
      window.addEventListener("popstate", () => this.route = normalizeRoute(window.location.pathname));
      window.setInterval(() => { this.ageTick++; this.refreshServiceGroups(); }, 1000);
      this.bindServiceGroupToggles();
      this.replaceRoute(this.authenticated && this.route === "/auth" ? "/overview" : this.authenticated ? this.route : "/auth");
      if (this.authenticated) await this.refresh();
    },
    bindServiceGroupToggles() {
      if (this.serviceGroupTogglesBound) return;
      this.serviceGroupTogglesBound = true;
      document.addEventListener("click", (event) => {
        const button = event.target.closest(".service-group-toggle");
        if (!button || !button.closest(".service-metric")) return;
        const panel = button.nextElementSibling;
        const isOpen = panel.classList.toggle("service-group-items-open");
        button.setAttribute("aria-expanded", String(isOpen));
        button.querySelector(".metric-chevron")?.classList.toggle("metric-chevron-open", isOpen);
      });
    },
    async login() {
      this.busy = true; this.error = "";
      try {
        saveToken(this.tokenInput);
        this.tokenInput = "";
        if (await this.refresh()) {
          this.authenticated = true;
          this.replaceRoute("/overview");
        }
      } catch (error) {
        clearSession(); this.authenticated = false; this.error = error.message;
      } finally { this.busy = false; }
    },
    logout() {
      clearSession(); this.authenticated = false; this.operator = {}; this.tokens = []; this.diagnostics = {}; this.serviceMetadata = {}; this.failedTaskDetails = []; this.oneTimeSecret = ""; this.replaceRoute("/auth");
    },
    async refresh() {
      this.busy = true; this.error = "";
      try {
        this.operator = await api.whoami();
        const [diagnostics, tokenResponse] = await Promise.all([api.diagnostics(), api.listTokens()]);
        this.diagnostics = diagnostics || {};
        this.services = this.diagnostics.services || {};
        this.serviceMetadata = this.diagnostics.service_metadata || {};
        this.refreshServiceGroups();
        this.failedTaskDetails = this.diagnostics.recent_failures || [];
        this.failedTasks = (this.diagnostics.task_summary || [])
          .filter((item) => item.status === "FAILED")
          .reduce((total, item) => total + Number(item.count || 0), 0);
        this.tokens = (tokenResponse?.items || []).map((item) => ({
          ...item,
          permissions_label: this.permissionHex(item.permissions),
          expires_label: this.formatDate(item.expires_at),
          revoked: Boolean(item.revoked_at),
        }));
        return true;
      } catch (error) {
        if (error.status === 401) {
          this.logout();
          this.error = "Invalid admin token";
        }
        else this.error = error.message;
        return false;
      } finally { this.busy = false; }
    },
    refreshServiceGroups() { this.serviceGroups = buildServiceGroups(this.services, this.serviceMetadata); },
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
    navigate(path) {
      const next = normalizeRoute(path);
      if (next === this.route) return;
      window.history.pushState({}, "", next);
      this.route = next;
    },
    replaceRoute(path) {
      const next = normalizeRoute(path);
      if (window.location.pathname !== next) window.history.replaceState({}, "", next);
      this.route = next;
    },
    showTokens() { this.navigate("/tokens"); },
    showDiagnostics() { this.navigate("/diagnostics"); },
    showOverview() { this.navigate("/overview"); },
    showAuth() { this.navigate("/auth"); },
    openCreate() { this.showCreate = true; },
    closeCreate() { this.showCreate = false; },
    dismissSecret() { this.oneTimeSecret = ""; },
    async revoke(event) {
      const item = this.tokens.find((token) => token.id === event.currentTarget.dataset.tokenId);
      if (!item || !window.confirm(`Revoke token ${item.name || item.id}?`)) return;
      this.busy = true; this.error = "";
      try { await api.revokeToken(item.id); await this.refresh(); } catch (error) { this.error = error.message; } finally { this.busy = false; }
    },
  };
}

function normalizeRoute(path) {
  if (path === "/auth" || path === "/tokens" || path === "/diagnostics") return path;
  return "/overview";
}

function buildServiceGroups(services, metadata) {
  const groups = new Map();
  for (const [name, status] of Object.entries(services)) {
    const serviceMetadata = metadata[name] || {};
    const path = String(serviceMetadata.group || `services.${name}`).split(".").filter(Boolean);
    const groupKey = path[0] || "services";
    const group = groups.get(groupKey) || { key: groupKey, label: humanize(groupKey), healthyCount: 0, services: [] };
    const childKey = path[path.length - 1] || name;
    group.services.push({
      key: name,
      label: serviceMetadata.display_name || humanize(childKey),
      status,
      age: formatAge(status.timestamp),
    });
    if (status.level === "OK") group.healthyCount++;
    group.countLabel = `(${group.healthyCount}/${group.services.length})`;
    groups.set(groupKey, group);
  }
  return [...groups.values()].sort((left, right) => left.label.localeCompare(right.label));
}

function humanize(value) {
  return String(value).split(/[-_]/).filter(Boolean).map((part) => part[0].toUpperCase() + part.slice(1)).join(" ");
}
