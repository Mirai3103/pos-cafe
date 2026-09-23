import { describe, expect, it } from "bun:test";
import {
  COMMIT_FAILURE_CODES,
  SUBMIT_ALREADY_DONE_CODES,
  useCommitDraft,
  usePayCash,
  useSubmitOrder,
} from "./use-checkout";

describe("use-checkout API seam exports", () => {
  it("exports the commit and cash payment hooks", () => {
    expect(typeof useCommitDraft).toBe("function");
    expect(typeof usePayCash).toBe("function");
  });
});

describe("COMMIT_FAILURE_CODES", () => {
  /**
   * Pinned against internal/sales/errors.go: a commit-time revalidation code
   * added or renamed on the backend without updating this set would silently
   * route the cashier down the generic error path instead of back to the draft.
   */
  const EXPECTED = [
    "EMPTY_DRAFT",
    "COMMIT_MENU_ITEM_UNAVAILABLE",
    "COMMIT_MENU_ITEM_RETIRED",
    "COMMIT_SIZE_REQUIRED",
    "COMMIT_SIZE_INVALID",
    "COMMIT_SIZE_UNAVAILABLE",
    "COMMIT_SIZE_RETIRED",
    "COMMIT_MODIFIER_OPTION_INVALID",
    "COMMIT_MODIFIER_OPTION_UNAVAILABLE",
    "COMMIT_MODIFIER_OPTION_RETIRED",
    "COMMIT_MODIFIER_GROUP_INVALID",
    "COMMIT_MODIFIER_GROUP_RETIRED",
  ];

  it("contains exactly the twelve commit revalidation codes", () => {
    expect(COMMIT_FAILURE_CODES.size).toBe(12);
    expect([...COMMIT_FAILURE_CODES].sort()).toEqual([...EXPECTED].sort());
  });
});

describe("submit seam", () => {
  it("exports the submit hook", () => {
    expect(typeof useSubmitOrder).toBe("function");
  });

  /**
   * NOTHING_TO_SUBMIT means another tab, or an attempt whose response was
   * lost, already submitted. Treating it as failure would strand the cashier
   * on a retry button that can never succeed.
   */
  it("treats exactly NOTHING_TO_SUBMIT as already done", () => {
    expect([...SUBMIT_ALREADY_DONE_CODES]).toEqual(["NOTHING_TO_SUBMIT"]);
  });
});
