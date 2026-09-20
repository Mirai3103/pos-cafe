import { describe, expect, it } from "bun:test";
import { toApiError } from "./api-client";
import { ApiError } from "./unwrap";

describe("toApiError", () => {
  it("carries the status and the server code", () => {
    const err = toApiError({
      isAxiosError: true,
      response: {
        status: 403,
        data: { success: false, error: { code: "FORBIDDEN", message: "phiên đang bị khóa" } },
      },
    });
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(403);
    expect(err.code).toBe("FORBIDDEN");
    expect(err.message).toBe("phiên đang bị khóa");
  });

  it("represents a transport failure with status 0", () => {
    const err = toApiError({ isAxiosError: true, message: "Network Error" });
    expect(err.status).toBe(0);
    expect(err.code).toBe("NETWORK_ERROR");
  });

  it("passes an ApiError through unchanged", () => {
    const original = new ApiError(429, "TOO_MANY_REQUESTS", "chờ chút");
    expect(toApiError(original)).toBe(original);
  });
});
