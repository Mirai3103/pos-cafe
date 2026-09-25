import { describe, expect, it } from "bun:test";
import { ApiError } from "./unwrap";
import { MENU_STALE_CODES, isMenuStaleError } from "./menu-staleness";

describe("isMenuStaleError", () => {
  it("recognises draft and commit availability refusals", () => {
    for (const code of [
      "MENU_ITEM_UNAVAILABLE",
      "SIZE_RETIRED",
      "MODIFIER_OPTION_UNAVAILABLE",
      "COMMIT_MENU_ITEM_UNAVAILABLE",
      "COMMIT_MODIFIER_GROUP_RETIRED",
    ]) {
      expect(isMenuStaleError(new ApiError(409, code, "x"))).toBe(true);
    }
  });

  it("ignores unrelated failures", () => {
    expect(isMenuStaleError(new ApiError(409, "EMPTY_DRAFT", "x"))).toBe(false);
    expect(isMenuStaleError(new Error("network"))).toBe(false);
  });

  it("holds exactly thirteen codes", () => {
    expect(MENU_STALE_CODES.size).toBe(13);
  });
});
