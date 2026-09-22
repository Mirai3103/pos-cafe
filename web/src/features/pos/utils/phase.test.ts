import { describe, expect, it } from "bun:test";
import {
  derivePosPhase,
  listLiveChecks,
  selectOpenCheck,
  hasMultipleOpenChecks,
  findCheckById,
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

  it("reports SETTLED once every Check is settled", () => {
    const settled = session({
      checks: [{ id: "check-1", state: "SETTLED", balance_vnd: 0, total_applied_vnd: 47_000 }],
    });
    expect(derivePosPhase(settled)).toBe("SETTLED");
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
    expect(derivePosPhase(merged)).toBe("SETTLED");
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
