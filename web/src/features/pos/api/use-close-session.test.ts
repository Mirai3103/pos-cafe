import { describe, expect, it } from "bun:test";
import { isCloseReady, recoverySessionId, useCloseFlow } from "./use-close-session";

describe("recoverySessionId", () => {
  it("reads back a session that closed somewhere else", () => {
    expect(recoverySessionId({ id: "s1", state: "CLOSED" }, false)).toBe("s1");
  });

  it("does not read back a session this terminal just closed", () => {
    expect(recoverySessionId({ id: "s1", state: "CLOSED" }, true)).toBeNull();
  });

  it("ignores active and missing sessions", () => {
    expect(recoverySessionId({ id: "s1", state: "ACTIVE" }, false)).toBeNull();
    expect(recoverySessionId(null, false)).toBeNull();
  });
});

describe("useCloseFlow", () => {
  it("is exported", () => {
    expect(typeof useCloseFlow).toBe("function");
  });
});

describe("isCloseReady", () => {
  it("defers to an explicit readiness when the caller supplies one", () => {
    expect(isCloseReady({ id: "s1", draft: { state: "EDITABLE", items: [] } }, true)).toBe(true);
    expect(isCloseReady(null, false)).toBe(false);
  });

  it("falls back to the takeaway phase", () => {
    expect(isCloseReady(null, undefined)).toBe(false);
  });
});
