import { describe, expect, it } from "bun:test";
import { resolveF9Action, usePosHotkeys } from "./use-pos-hotkeys";

describe("resolveF9Action", () => {
  it("follows the primary action of each phase", () => {
    expect(resolveF9Action("NO_SESSION", false)).toBe("checkout");
    expect(resolveF9Action("DRAFTING", false)).toBe("checkout");
    expect(resolveF9Action("AWAITING_PAYMENT", false)).toBe("checkout");
    expect(resolveF9Action("AWAITING_SUBMIT", false)).toBe("submit");
    expect(resolveF9Action("IN_PREPARATION", false)).toBe("next-customer");
    expect(resolveF9Action("READY_TO_CLOSE", false)).toBe("close");
  });

  it("does nothing while a dialog, the picker, or the drawer is open", () => {
    expect(resolveF9Action("READY_TO_CLOSE", true)).toBe("none");
    expect(resolveF9Action("DRAFTING", true)).toBe("none");
  });
});

describe("usePosHotkeys", () => {
  it("is exported", () => {
    expect(typeof usePosHotkeys).toBe("function");
  });
});
