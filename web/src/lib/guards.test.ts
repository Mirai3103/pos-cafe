import { beforeEach, describe, expect, it } from "bun:test";
import { isRedirect } from "@tanstack/react-router";
import { useSessionStore } from "@/stores/use-session-store";
import { requireAnyCapability, requireCapability } from "./guards";

function thrown(fn: () => void): unknown {
  try {
    fn();
  } catch (e) {
    return e;
  }
  return undefined;
}

function redirectTarget(e: unknown): string | undefined {
  return isRedirect(e) ? (e.options.to as string) : undefined;
}

describe("requireAnyCapability", () => {
  beforeEach(() => {
    useSessionStore.setState({ state: "authenticated", capabilities: ["catalog.manage_availability"] });
  });

  it("passes when any listed capability is held", () => {
    expect(thrown(() => requireAnyCapability(["staff.administer", "catalog.manage_availability"]))).toBeUndefined();
  });

  it("redirects to /no-access when none is held", () => {
    expect(redirectTarget(thrown(() => requireAnyCapability(["staff.administer"])))).toBe("/no-access");
  });

  it("does not evaluate capabilities while locked", () => {
    useSessionStore.setState({ state: "locked", capabilities: [] });
    expect(thrown(() => requireAnyCapability(["staff.administer"]))).toBeUndefined();
  });

  it("sends a signed-out session to login", () => {
    useSessionStore.setState({ state: "signed_out", capabilities: [] });
    expect(redirectTarget(thrown(() => requireAnyCapability(["x"])))).toBe("/auth/login");
  });

  it("keeps requireCapability behaving as before", () => {
    expect(thrown(() => requireCapability("catalog.manage_availability"))).toBeUndefined();
    expect(redirectTarget(thrown(() => requireCapability("staff.administer")))).toBe("/no-access");
  });
});
