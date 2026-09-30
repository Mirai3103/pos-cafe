import { describe, expect, it } from "bun:test";
import {
  derivePosPhase,
  listLiveChecks,
  selectOpenCheck,
  hasMultipleOpenChecks,
  findCheckById,
  isTerminalUnit,
  preparationProgress,
  hasUnsubmittedWork,
  isPostPaymentPhase,
} from "./phase";
import type { SalesServiceSessionResponse } from "@/api/generated/models";

function session(
  overrides: Partial<SalesServiceSessionResponse>,
): SalesServiceSessionResponse {
  return {
    id: "session-1",
    service_number: "007",
    service_mode: "TAKEAWAY",
    state: "ACTIVE",
    sales_shift_id: "shift-1",
    tables: [],
    checks: [],
    orders: [],
    preparation_units: [],
    ...overrides,
  };
}

const editableDraft = { id: "draft-1", state: "EDITABLE", check_target: "SAME_CHECK", items: [] };

describe("derivePosPhase", () => {
  it("reports NO_SESSION without a session", () => {
    expect(derivePosPhase(null)).toBe("NO_SESSION");
    expect(derivePosPhase(undefined)).toBe("NO_SESSION");
  });

  it("reports DRAFTING while an editable draft stands", () => {
    expect(derivePosPhase(session({ draft: editableDraft }))).toBe("DRAFTING");
  });

  it("reports AWAITING_PAYMENT once the draft is committed and a Check is open", () => {
    const committed = session({
      checks: [{ id: "check-1", state: "OPEN", balance_vnd: 47_000, created_at: "2026-09-24T01:00:00Z" }],
    });
    expect(derivePosPhase(committed)).toBe("AWAITING_PAYMENT");
  });

  it("reports READY_TO_CLOSE for a settled session with nothing unsubmitted and no units", () => {
    const settled = session({
      checks: [{ id: "check-1", state: "SETTLED", balance_vnd: 0, total_applied_vnd: 47_000 }],
    });
    expect(derivePosPhase(settled)).toBe("READY_TO_CLOSE");
  });

  it("reports AWAITING_SUBMIT while a settled Check carries unsubmitted work", () => {
    const paid = session({
      checks: [
        {
          id: "check-1",
          state: "SETTLED",
          balance_vnd: 0,
          allocations: [{ id: "a1", submitted: false }],
        },
      ],
    });
    expect(derivePosPhase(paid)).toBe("AWAITING_SUBMIT");
  });

  it("reports IN_PREPARATION once submitted while a unit is still in the kitchen", () => {
    const submitted = session({
      checks: [{ id: "check-1", state: "SETTLED", balance_vnd: 0, allocations: [{ id: "a1", submitted: true }] }],
      preparation_units: [
        { id: "u1", state: "FULFILLED" },
        { id: "u2", state: "IN_PREPARATION" },
      ],
    });
    expect(derivePosPhase(submitted)).toBe("IN_PREPARATION");
  });

  it("reports READY_TO_CLOSE when every unit is terminal", () => {
    const done = session({
      checks: [{ id: "check-1", state: "SETTLED", balance_vnd: 0, allocations: [{ id: "a1", submitted: true }] }],
      preparation_units: [
        { id: "u1", state: "FULFILLED" },
        { id: "u2", state: "CANCELLED" },
        { id: "u3", state: "WASTED" },
      ],
    });
    expect(derivePosPhase(done)).toBe("READY_TO_CLOSE");
  });

  it("keeps IN_PREPARATION while the remake of a wasted unit is queued", () => {
    const remaking = session({
      checks: [{ id: "check-1", state: "SETTLED", balance_vnd: 0, allocations: [{ id: "a1", submitted: true }] }],
      preparation_units: [
        { id: "u1", state: "WASTED" },
        { id: "u2", state: "QUEUED", remake_of_preparation_unit_id: "u1" },
      ],
    });
    expect(derivePosPhase(remaking)).toBe("IN_PREPARATION");
  });

  it("reports AWAITING_PAYMENT when a settled Check sits beside an open one", () => {
    const mixed = session({
      checks: [
        { id: "check-1", state: "SETTLED", balance_vnd: 0 },
        { id: "check-2", state: "OPEN", balance_vnd: 12_000 },
      ],
    });
    expect(derivePosPhase(mixed)).toBe("AWAITING_PAYMENT");
  });

  it("falls back to NO_SESSION when the draft is gone and no Check exists", () => {
    expect(derivePosPhase(session({ checks: [] }))).toBe("NO_SESSION");
  });

  it("ignores a Check that was merged away", () => {
    const merged = session({
      checks: [
        { id: "check-1", state: "MERGED", merged_into_check_id: "check-2" },
        { id: "check-2", state: "SETTLED", balance_vnd: 0 },
      ],
    });
    expect(derivePosPhase(merged)).toBe("READY_TO_CLOSE");
  });
});

describe("selectOpenCheck", () => {
  it("returns the oldest open Check", () => {
    const s = session({
      checks: [
        { id: "newer", state: "OPEN", created_at: "2026-09-24T03:00:00Z" },
        { id: "older", state: "OPEN", created_at: "2026-09-24T01:00:00Z" },
      ],
    });
    expect(selectOpenCheck(s)?.id).toBe("older");
  });

  it("returns null when nothing is open", () => {
    expect(selectOpenCheck(session({ checks: [{ id: "c", state: "SETTLED" }] }))).toBeNull();
    expect(selectOpenCheck(null)).toBeNull();
  });
});

describe("hasMultipleOpenChecks", () => {
  it("is true only with more than one open Check", () => {
    const one = session({ checks: [{ id: "a", state: "OPEN" }] });
    const two = session({ checks: [{ id: "a", state: "OPEN" }, { id: "b", state: "OPEN" }] });
    expect(hasMultipleOpenChecks(one)).toBe(false);
    expect(hasMultipleOpenChecks(two)).toBe(true);
  });
});

describe("listLiveChecks and findCheckById", () => {
  it("drops merged Checks and finds by id", () => {
    const s = session({
      checks: [
        { id: "gone", state: "MERGED", merged_into_check_id: "kept" },
        { id: "kept", state: "SETTLED" },
      ],
    });
    expect(listLiveChecks(s).map((c) => c.id)).toEqual(["kept"]);
    expect(findCheckById(s, "kept")?.id).toBe("kept");
    expect(findCheckById(s, "gone")).toBeNull();
  });
});

describe("preparation helpers", () => {
  it("treats FULFILLED, CANCELLED and WASTED as terminal", () => {
    expect(isTerminalUnit({ state: "FULFILLED" })).toBe(true);
    expect(isTerminalUnit({ state: "CANCELLED" })).toBe(true);
    expect(isTerminalUnit({ state: "WASTED" })).toBe(true);
    expect(isTerminalUnit({ state: "QUEUED" })).toBe(false);
    expect(isTerminalUnit({ state: "READY" })).toBe(false);
    expect(isTerminalUnit({})).toBe(false);
  });

  it("counts terminal units over all units", () => {
    const s = session({
      preparation_units: [{ state: "FULFILLED" }, { state: "READY" }, { state: "WASTED" }],
    });
    expect(preparationProgress(s)).toEqual({ done: 2, total: 3 });
    expect(preparationProgress(null)).toEqual({ done: 0, total: 0 });
  });

  it("finds unsubmitted work only on live Checks", () => {
    const merged = session({
      checks: [
        { id: "c1", state: "MERGED", merged_into_check_id: "c2", allocations: [{ submitted: false }] },
        { id: "c2", state: "SETTLED", allocations: [{ submitted: true }] },
      ],
    });
    expect(hasUnsubmittedWork(merged)).toBe(false);
    expect(
      hasUnsubmittedWork(session({ checks: [{ id: "c1", state: "SETTLED", allocations: [{}] }] })),
    ).toBe(true);
  });

  it("groups the three phases that follow payment", () => {
    expect(isPostPaymentPhase("AWAITING_SUBMIT")).toBe(true);
    expect(isPostPaymentPhase("IN_PREPARATION")).toBe(true);
    expect(isPostPaymentPhase("READY_TO_CLOSE")).toBe(true);
    expect(isPostPaymentPhase("AWAITING_PAYMENT")).toBe(false);
    expect(isPostPaymentPhase("DRAFTING")).toBe(false);
    expect(isPostPaymentPhase("NO_SESSION")).toBe(false);
  });
});
