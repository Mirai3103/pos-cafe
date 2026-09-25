import { describe, expect, it } from "bun:test";
import { SETTINGS_CAPABILITIES, firstPermittedTab, permittedTabs } from "./tabs";

describe("settings tabs", () => {
  it("lists availability for anyone who manages availability", () => {
    expect(firstPermittedTab(["catalog.manage_availability"])?.to).toBe("/settings/availability");
    expect(permittedTabs(["catalog.manage_availability"]).map((t) => t.label)).toEqual(["Món tạm hết"]);
  });

  it("has no tab for a session without a settings capability", () => {
    expect(firstPermittedTab(["sales.operate"])).toBeNull();
  });

  it("exposes every tab capability for the layout guard", () => {
    expect(SETTINGS_CAPABILITIES).toEqual(["catalog.manage_availability"]);
  });
});
