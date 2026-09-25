import { describe, expect, it } from "bun:test";
import type { SalesServiceSessionResponse } from "@/api/generated/models";
import {
  canCollect,
  deriveDineInStatus,
  dineInPhase,
  isDineIn,
  needsNewRound,
  planSendToBar,
  resolveDineInF9,
  sessionTableLabel,
  shouldPollSession,
} from "./dine-in";

type Session = SalesServiceSessionResponse;
type Check = NonNullable<Session["checks"]>[number];

function allocation(submitted: boolean) {
  return { id: "a1", name: "Bạc xỉu", allocated_quantity: 1, amount_vnd: 30_000, submitted };
}

function check(state: "OPEN" | "SETTLED", submitted: boolean, extra: Partial<Check> = {}): Check {
  return {
    id: `check-${state}`,
    state,
    charge_vnd: 30_000,
    balance_vnd: state === "OPEN" ? 30_000 : 0,
    created_at: "2026-09-27T01:00:00Z",
    allocations: [allocation(submitted)],
    payments: [],
    ...extra,
  };
}

function session(overrides: Partial<Session> = {}): Session {
  return {
    id: "s1",
    service_number: "012",
    service_mode: "DINE_IN",
    state: "ACTIVE",
    tables: [
      { id: "t1", name: "Bàn 1" },
      { id: "t2", name: "Bàn 2" },
    ],
    draft: null,
    checks: [],
    orders: [],
    preparation_units: [],
    ...overrides,
  } as Session;
}

const editableDraft = (count: number) => ({
  state: "EDITABLE",
  items: Array.from({ length: count }, (_, i) => ({ id: `d${i}`, name: "Trà đào", quantity: 1 })),
});

const order = { id: "o1" };
const unit = (state: string) => ({ id: `u-${state}`, state });

describe("deriveDineInStatus", () => {
  it("reads a fresh Session: empty editable draft, nothing to do", () => {
    const status = deriveDineInStatus(session({ draft: editableDraft(0) } as Partial<Session>));
    expect(status.hasEditableDraft).toBe(true);
    expect(status.draftItemCount).toBe(0);
    expect(status.openCheck).toBeNull();
    expect(status.canClose).toBe(false);
    expect(status.canOrder).toBe(true);
  });

  it("counts draft items only while the draft is editable", () => {
    expect(deriveDineInStatus(session({ draft: editableDraft(2) } as Partial<Session>)).draftItemCount).toBe(2);
    expect(
      deriveDineInStatus(session({ draft: { state: "COMMITTED", items: [{ id: "x" }] } } as Partial<Session>))
        .draftItemCount,
    ).toBe(0);
  });

  it("blocks ordering while a committed round has not been submitted", () => {
    const status = deriveDineInStatus(session({ checks: [check("OPEN", false)] }));
    expect(status.hasUnsubmittedWork).toBe(true);
    expect(status.canOrder).toBe(false);
  });

  it("exposes the open Check once a round is submitted but unpaid", () => {
    const status = deriveDineInStatus(
      session({ checks: [check("OPEN", true)], orders: [order], preparation_units: [unit("QUEUED")] } as Partial<Session>),
    );
    expect(status.openCheck?.id).toBe("check-OPEN");
    expect(status.canOrder).toBe(true);
    expect(status.canClose).toBe(false);
  });

  it("can close when settled, submitted, ordered, and every unit terminal", () => {
    const status = deriveDineInStatus(
      session({
        checks: [check("SETTLED", true)],
        orders: [order],
        preparation_units: [unit("FULFILLED"), unit("WASTED")],
      } as Partial<Session>),
    );
    expect(status.canClose).toBe(true);
  });

  it("does not let an empty editable draft block closure", () => {
    const status = deriveDineInStatus(
      session({
        draft: editableDraft(0),
        checks: [check("SETTLED", true)],
        orders: [order],
        preparation_units: [unit("FULFILLED")],
      } as Partial<Session>),
    );
    expect(status.canClose).toBe(true);
  });

  it("refuses closure while a unit is still in preparation", () => {
    const status = deriveDineInStatus(
      session({ checks: [check("SETTLED", true)], orders: [order], preparation_units: [unit("READY")] } as Partial<Session>),
    );
    expect(status.canClose).toBe(false);
  });

  it("refuses closure while a refund is pending", () => {
    const status = deriveDineInStatus(
      session({
        checks: [check("SETTLED", true, { pending_refund_vnd: 5_000 })],
        orders: [order],
        preparation_units: [unit("FULFILLED")],
      } as Partial<Session>),
    );
    expect(status.canClose).toBe(false);
  });

  it("flags more than one open Check", () => {
    const status = deriveDineInStatus(
      session({
        checks: [check("OPEN", true), { ...check("OPEN", true), id: "check-2" }],
        orders: [order],
      } as Partial<Session>),
    );
    expect(status.hasMultipleOpenChecks).toBe(true);
    expect(canCollect(status)).toBe(false);
  });
});

describe("dineInPhase", () => {
  it("prefers closure, then unsent work, then payment, then the bar, then drafting", () => {
    const ready = deriveDineInStatus(
      session({ checks: [check("SETTLED", true)], orders: [order], preparation_units: [unit("FULFILLED")] } as Partial<Session>),
    );
    expect(dineInPhase(ready)).toBe("READY_TO_CLOSE");

    const drafting = deriveDineInStatus(
      session({ draft: editableDraft(1), checks: [check("OPEN", true)], orders: [order] } as Partial<Session>),
    );
    expect(dineInPhase(drafting)).toBe("AWAITING_SUBMIT");

    const unpaid = deriveDineInStatus(
      session({ checks: [check("OPEN", true)], orders: [order], preparation_units: [unit("QUEUED")] } as Partial<Session>),
    );
    expect(dineInPhase(unpaid)).toBe("AWAITING_PAYMENT");

    const cooking = deriveDineInStatus(
      session({ checks: [check("SETTLED", true)], orders: [order], preparation_units: [unit("QUEUED")] } as Partial<Session>),
    );
    expect(dineInPhase(cooking)).toBe("IN_PREPARATION");

    expect(dineInPhase(deriveDineInStatus(session({ draft: editableDraft(0) } as Partial<Session>)))).toBe("DRAFTING");
  });
});

describe("planSendToBar", () => {
  it("commits and submits a drafted round", () => {
    expect(planSendToBar(deriveDineInStatus(session({ draft: editableDraft(2) } as Partial<Session>)))).toEqual({
      commit: true,
      submit: true,
    });
  });

  it("only submits a round whose commit already landed", () => {
    expect(planSendToBar(deriveDineInStatus(session({ checks: [check("OPEN", false)] })))).toEqual({
      commit: false,
      submit: true,
    });
  });

  it("does nothing when there is nothing to send", () => {
    expect(planSendToBar(deriveDineInStatus(session({ draft: editableDraft(0) } as Partial<Session>)))).toEqual({
      commit: false,
      submit: false,
    });
  });
});

describe("canCollect", () => {
  it("needs one open Check and an empty draft", () => {
    const base = { checks: [check("OPEN", true)], orders: [order] };
    expect(canCollect(deriveDineInStatus(session(base as Partial<Session>)))).toBe(true);
    expect(
      canCollect(deriveDineInStatus(session({ ...base, draft: editableDraft(1) } as Partial<Session>))),
    ).toBe(false);
    expect(canCollect(deriveDineInStatus(session({ draft: editableDraft(0) } as Partial<Session>)))).toBe(false);
  });
});

describe("resolveDineInF9", () => {
  it("fires send before collect before close, and nothing when blocked", () => {
    const sendable = deriveDineInStatus(session({ draft: editableDraft(1), checks: [check("OPEN", true)], orders: [order] } as Partial<Session>));
    expect(resolveDineInF9(sendable, false)).toBe("send");
    expect(resolveDineInF9(sendable, true)).toBe("none");

    const collectable = deriveDineInStatus(session({ checks: [check("OPEN", true)], orders: [order] } as Partial<Session>));
    expect(resolveDineInF9(collectable, false)).toBe("collect");

    const closable = deriveDineInStatus(
      session({ checks: [check("SETTLED", true)], orders: [order], preparation_units: [unit("FULFILLED")] } as Partial<Session>),
    );
    expect(resolveDineInF9(closable, false)).toBe("close");

    expect(resolveDineInF9(deriveDineInStatus(session({ draft: editableDraft(0) } as Partial<Session>)), false)).toBe("none");
  });
});

describe("needsNewRound", () => {
  it("opens a round only for a dine-in Session with no editable draft and nothing unsent", () => {
    expect(needsNewRound(session({ checks: [check("OPEN", true)], orders: [order] } as Partial<Session>))).toBe(true);
    expect(needsNewRound(session({ draft: editableDraft(0) } as Partial<Session>))).toBe(false);
    expect(needsNewRound(session({ checks: [check("OPEN", false)] }))).toBe(false);
    expect(needsNewRound(session({ service_mode: "TAKEAWAY" }))).toBe(false);
    expect(needsNewRound(null)).toBe(false);
  });
});

describe("shouldPollSession", () => {
  it("polls a dine-in Session with unfinished units even while a draft is open", () => {
    expect(
      shouldPollSession(session({ draft: editableDraft(1), orders: [order], preparation_units: [unit("QUEUED")] } as Partial<Session>)),
    ).toBe(true);
    expect(shouldPollSession(session({ draft: editableDraft(0) } as Partial<Session>))).toBe(false);
  });

  it("keeps the takeaway rule", () => {
    expect(
      shouldPollSession(
        session({ service_mode: "TAKEAWAY", checks: [check("SETTLED", true)], orders: [order], preparation_units: [unit("QUEUED")] } as Partial<Session>),
      ),
    ).toBe(true);
  });
});

describe("labels", () => {
  it("names Tables in order, or says there are none", () => {
    expect(sessionTableLabel(session())).toBe("Bàn 1, Bàn 2");
    expect(sessionTableLabel(session({ tables: [] }))).toBe("Chưa có bàn");
  });

  it("recognises dine-in", () => {
    expect(isDineIn(session())).toBe(true);
    expect(isDineIn(session({ service_mode: "TAKEAWAY" }))).toBe(false);
    expect(isDineIn(null)).toBe(false);
  });
});
