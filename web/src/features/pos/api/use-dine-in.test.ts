import { describe, expect, it } from "bun:test";
import { ApiError } from "@/lib/unwrap";
import { classifySendFailure, useDineInFlow } from "./use-dine-in";

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
