import { describe, expect, it } from "bun:test";
import { ApiError, isConflictError, unwrap, unwrapNullable } from "./unwrap";

describe("unwrap", () => {
  it("returns the payload of a successful envelope", () => {
    expect(unwrap({ success: true, data: { id: "s1" } })).toEqual({ id: "s1" });
  });

  it("throws ApiError carrying the server code and message", () => {
    expect(() =>
      unwrap({ success: false, error: { code: "UNAUTHORIZED", message: "sai mã PIN" } }),
    ).toThrow(ApiError);
  });

  it("preserves the server code on the thrown error", () => {
    try {
      unwrap({ success: false, error: { code: "TOO_MANY_REQUESTS", message: "thử lại sau" } });
      throw new Error("unwrap should have thrown");
    } catch (error) {
      expect(error).toBeInstanceOf(ApiError);
      expect((error as ApiError).code).toBe("TOO_MANY_REQUESTS");
      expect((error as ApiError).message).toBe("thử lại sau");
    }
  });

  it("throws when success is true but data is absent", () => {
    expect(() => unwrap({ success: true })).toThrow(ApiError);
  });

  it("does not treat a false-y payload as absent", () => {
    expect(unwrap<number>({ success: true, data: 0 })).toBe(0);
  });
});

describe("unwrapNullable", () => {
  it("returns the payload of a successful envelope", () => {
    expect(unwrapNullable({ success: true, data: { id: "s1" } })).toEqual({ id: "s1" });
  });

  it("returns null when data is null", () => {
    expect(unwrapNullable({ success: true, data: null })).toBeNull();
  });

  it("returns null when data is undefined / absent", () => {
    expect(unwrapNullable({ success: true })).toBeNull();
  });

  it("throws ApiError when success is false", () => {
    expect(() =>
      unwrapNullable({ success: false, error: { code: "UNAUTHORIZED", message: "sai mã PIN" } }),
    ).toThrow(ApiError);
  });

  it("does not treat a false-y payload as null", () => {
    expect(unwrapNullable<number>({ success: true, data: 0 })).toBe(0);
  });
});

describe("isConflictError", () => {
  it("is true for an HTTP 409 ApiError", () => {
    expect(isConflictError(new ApiError(409, "UNFULFILLED_PREPARATION_FOR_CLOSURE", "x"))).toBe(true);
  });

  it("is false for other statuses and for non-ApiErrors", () => {
    expect(isConflictError(new ApiError(404, "SERVICE_SESSION_NOT_FOUND", "x"))).toBe(false);
    expect(isConflictError(new ApiError(0, "NETWORK_ERROR", "x"))).toBe(false);
    expect(isConflictError(new Error("boom"))).toBe(false);
  });
});

