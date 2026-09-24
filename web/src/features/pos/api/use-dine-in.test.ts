import { describe, expect, it } from "bun:test";
import { ApiError } from "@/lib/unwrap";
import {
  classifySendFailure,
  isPaymentSnapshotStale,
  PAYMENT_DIALOG_RESET_STATE,
  STALE_PAYMENT_TOTAL_MESSAGE,
  useDineInFlow,
  type PaymentSnapshot,
} from "./use-dine-in";

describe("classifySendFailure", () => {
  it("sends commit revalidation failures back to the draft", () => {
    expect(classifySendFailure(new ApiError(409, "COMMIT_SIZE_REQUIRED", "x"))).toBe("draft");
    expect(classifySendFailure(new ApiError(422, "EMPTY_DRAFT", "x"))).toBe("draft");
  });

  it("treats NOTHING_TO_SUBMIT as already sent", () => {
    expect(classifySendFailure(new ApiError(409, "NOTHING_TO_SUBMIT", "x"))).toBe("done");
  });

  it("leaves anything else on the retry button", () => {
    expect(classifySendFailure(new ApiError(500, "INTERNAL_ERROR", "x"))).toBe("retry");
    expect(classifySendFailure(new Error("network"))).toBe("retry");
  });
});

describe("useDineInFlow", () => {
  it("is exported", () => {
    expect(typeof useDineInFlow).toBe("function");
  });
});

/**
 * confirmPayment's guard against a Check that changed under the dialog
 * (Fix 2 of the slice 7 final-review pass): another terminal sending a round
 * into the same Check, or a different Check opening altogether, must never
 * be charged at the total the dialog captured on open.
 */
describe("isPaymentSnapshotStale", () => {
  const captured: PaymentSnapshot = { checkId: "c1", balanceVnd: 30_000 };

  it("is not stale when nothing was captured yet", () => {
    expect(isPaymentSnapshotStale(null, { id: "c1", balance_vnd: 30_000 })).toBe(false);
  });

  it("is not stale when the same Check still carries the captured balance", () => {
    expect(isPaymentSnapshotStale(captured, { id: "c1", balance_vnd: 30_000 })).toBe(false);
  });

  it("is stale when another terminal changed the balance of the same Check", () => {
    expect(isPaymentSnapshotStale(captured, { id: "c1", balance_vnd: 45_000 })).toBe(true);
  });

  it("is stale when a different Check opened", () => {
    expect(isPaymentSnapshotStale(captured, { id: "c2", balance_vnd: 30_000 })).toBe(true);
  });

  it("is stale when the Check disappeared (paid off elsewhere, or settled)", () => {
    expect(isPaymentSnapshotStale(captured, null)).toBe(true);
    expect(isPaymentSnapshotStale(captured, { id: null, balance_vnd: 0 })).toBe(true);
  });
});

describe("STALE_PAYMENT_TOTAL_MESSAGE", () => {
  it("is a user-facing Vietnamese message, not an error code", () => {
    expect(STALE_PAYMENT_TOTAL_MESSAGE.length).toBeGreaterThan(0);
    expect(STALE_PAYMENT_TOTAL_MESSAGE).not.toMatch(/^[A-Z_]+$/);
  });
});

/**
 * Fix 1 of the slice 7 final-review pass: the `[activeSessionId]` effect in
 * useDineInFlow applies exactly this shape to the payment-dialog fields, so a
 * dropped session pointer (shouldDropSessionPointer fires on any transient
 * read error, including one mid-poll while a bill is being collected) can
 * never leave the dialog open, paying, erroring, or showing a stale total for
 * the Session that replaces it. This suite has no DOM/renderHook harness
 * (none exists anywhere in this repo — see use-pos-session.test.ts and
 * use-close-session.test.ts for the same pure-predicate pattern), so the
 * contract is pinned here at the value level; the effect itself sources these
 * same constants, so a regression in either shows up as a one-line diff.
 */
describe("PAYMENT_DIALOG_RESET_STATE", () => {
  it("closes the dialog and clears every payment field a stale Session could leak", () => {
    expect(PAYMENT_DIALOG_RESET_STATE).toEqual({
      isPaymentOpen: false,
      isPaying: false,
      paymentError: null,
      changeDueVnd: null,
      dialogTotalVnd: 0,
    });
  });
});
