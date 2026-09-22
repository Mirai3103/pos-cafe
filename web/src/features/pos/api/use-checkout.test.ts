import { describe, expect, it } from "bun:test";
import { useCommitDraft, usePayCash } from "./use-checkout";

describe("use-checkout API seam exports", () => {
  it("exports the commit and cash payment hooks", () => {
    expect(typeof useCommitDraft).toBe("function");
    expect(typeof usePayCash).toBe("function");
  });
});
