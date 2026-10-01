import { describe, expect, it } from "bun:test";
import { ApiError } from "@/lib/unwrap";
import {
  AVAILABILITY_POLL_MS,
  STALE_RESTORE_MESSAGE,
  classifyRestoreFailure,
  useAvailabilityMenu,
  useRestoreAvailability,
  useSetAvailability,
} from "./use-availability";

describe("classifyRestoreFailure", () => {
  it("treats a changed or missing entity as a stale list", () => {
    expect(classifyRestoreFailure(new ApiError(409, "ENTITY_RETIRED", "x"))).toBe("stale");
    expect(classifyRestoreFailure(new ApiError(409, "REQUEST_CONFLICT", "x"))).toBe("stale");
    expect(classifyRestoreFailure(new ApiError(404, "CATALOG_NOT_FOUND", "x"))).toBe("stale");
  });

  it("keeps the same intent for anything else", () => {
    expect(classifyRestoreFailure(new ApiError(500, "INTERNAL_ERROR", "x"))).toBe("retry");
    expect(classifyRestoreFailure(new Error("network"))).toBe("retry");
  });
});

describe("availability hooks", () => {
  it("poll every 15 seconds and export their hooks", () => {
    expect(AVAILABILITY_POLL_MS).toBe(15_000);
    expect(STALE_RESTORE_MESSAGE).toBe("Danh sách đã thay đổi, vui lòng kiểm tra lại");
    expect(typeof useAvailabilityMenu).toBe("function");
    expect(typeof useSetAvailability).toBe("function");
    expect(typeof useRestoreAvailability).toBe("function");
  });
});
