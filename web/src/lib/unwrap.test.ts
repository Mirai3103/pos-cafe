import { describe, expect, it } from "bun:test";
import { ApiError, unwrap } from "./unwrap";

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
