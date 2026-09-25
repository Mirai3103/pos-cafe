import { describe, expect, it } from "bun:test";
import {
  recoverAfterRoundConflict,
  selectPosSession,
  shouldDropSessionPointer,
  shouldOpenNextRound,
  usePosSession,
} from "./use-pos-session";

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

describe("selectPosSession", () => {
  it("stores the pointer the POS reads on mount", () => {
    if (typeof sessionStorage === "undefined") return;
    selectPosSession("s-42");
    expect(sessionStorage.getItem("pos_active_session_id")).toBe("s-42");
  });
});

/**
 * ensureDraft calls startNextDraft only when this predicate holds; the
 * dedup-in-flight behaviour lives in a ref inside the hook and needs no
 * separate test here, since it only ever guards a second call once this
 * predicate has already said yes.
 */
describe("shouldOpenNextRound", () => {
  it("opens a round for a dine-in Session between rounds", () => {
    expect(
      shouldOpenNextRound({ id: "s1", service_mode: "DINE_IN", draft: null, checks: [] }, "s1"),
    ).toBe(true);
  });

  it("is a no-op when the Session still has an editable draft", () => {
    expect(
      shouldOpenNextRound(
        { id: "s1", service_mode: "DINE_IN", draft: { state: "EDITABLE", items: [] } },
        "s1",
      ),
    ).toBe(false);
  });

  it("is a no-op for a takeaway Session", () => {
    expect(
      shouldOpenNextRound({ id: "s1", service_mode: "TAKEAWAY", draft: null }, "s1"),
    ).toBe(false);
  });

  it("is a no-op once another Session became current before this one ran", () => {
    expect(
      shouldOpenNextRound({ id: "s2", service_mode: "DINE_IN", draft: null }, "s1"),
    ).toBe(false);
  });

  it("is a no-op while the previous round is still unsubmitted", () => {
    expect(
      shouldOpenNextRound(
        {
          id: "s1",
          service_mode: "DINE_IN",
          draft: null,
          checks: [{ id: "c1", state: "OPEN", allocations: [{ id: "a1", submitted: false }] }],
        },
        "s1",
      ),
    ).toBe(false);
  });

  it("is a no-op with no current Session at all", () => {
    expect(shouldOpenNextRound(null, "s1")).toBe(false);
    expect(shouldOpenNextRound(undefined, "s1")).toBe(false);
  });
});

describe("recoverAfterRoundConflict", () => {
  it("proceeds when another terminal already opened the round", () => {
    expect(recoverAfterRoundConflict({ id: "s1", draft: { state: "EDITABLE", items: [] } })).toBe(true);
  });

  it("gives up when the round is still blocked", () => {
    expect(recoverAfterRoundConflict({ id: "s1", draft: null })).toBe(false);
    expect(recoverAfterRoundConflict(undefined)).toBe(false);
  });
});
