import { describe, expect, it } from "bun:test";
import { recoverySessionId, useCloseFlow } from "./use-close-session";

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
