import { describe, expect, it } from "bun:test";
import { shouldDropSessionPointer, usePosSession } from "./use-pos-session";

describe("shouldDropSessionPointer", () => {
  it("keeps an active session", () => {
    expect(shouldDropSessionPointer({ id: "s1", state: "ACTIVE" }, false)).toBe(false);
  });

  it("keeps a closed session so its Completed Sale can be shown", () => {
    expect(shouldDropSessionPointer({ id: "s1", state: "CLOSED" }, false)).toBe(false);
  });

  it("keeps the pointer while the session is still loading", () => {
    expect(shouldDropSessionPointer(null, false)).toBe(false);
    expect(shouldDropSessionPointer({ id: "s1" }, false)).toBe(false);
  });

  it("drops the pointer on a read error or an unknown state", () => {
    expect(shouldDropSessionPointer(null, true)).toBe(true);
    expect(shouldDropSessionPointer({ id: "s1", state: "ABANDONED" }, false)).toBe(true);
  });
});

describe("usePosSession", () => {
  it("is exported", () => {
    expect(typeof usePosSession).toBe("function");
  });
});
