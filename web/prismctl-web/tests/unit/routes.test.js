import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { controlRoom } from "../../src/app.js";

describe("Control Room routes", () => {
  beforeEach(() => {
    sessionStorage.clear();
    vi.spyOn(window, "setInterval").mockReturnValue(0);
  });

  afterEach(() => vi.restoreAllMocks());

  it("normalizes unknown and unauthenticated routes to auth", () => {
    window.history.replaceState({}, "", "/unknown");
    const state = controlRoom();

    expect(state.route).toBe("/overview");
    state.init();
    expect(state.route).toBe("/auth");
    expect(window.location.pathname).toBe("/auth");
  });

  it("navigates between protected views without leaving the SPA", () => {
    window.history.replaceState({}, "", "/overview");
    sessionStorage.setItem("prismctl.adminToken", "padm_test");
    const state = controlRoom();

    state.showTokens();
    expect(state.route).toBe("/tokens");
    expect(window.location.pathname).toBe("/tokens");

    state.showOverview();
    expect(state.route).toBe("/overview");
    expect(window.location.pathname).toBe("/overview");
  });

  it("groups service statuses from API metadata", () => {
    const state = controlRoom();
    state.services = {
      "batch-detector": { level: "OK", timestamp: new Date().toISOString() },
      "scheduler-fast": { level: "ERROR", timestamp: new Date().toISOString() },
    };
    state.serviceMetadata = {
      "batch-detector": { group: "batch.detector" },
      "scheduler-fast": { group: "scheduler.fast" },
    };
    state.refreshServiceGroups();

    expect(state.serviceGroups.map((group) => group.label)).toEqual(["Batch", "Scheduler"]);
    expect(state.serviceGroups[0].services[0].label).toBe("Detector");
    expect(state.serviceGroups[0].healthyCount).toBe(1);
    state.toggleServices();
    expect(state.servicesExpanded).toBe(true);
  });
});
