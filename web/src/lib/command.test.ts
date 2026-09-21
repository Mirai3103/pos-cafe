import { describe, expect, it } from "bun:test";
import { newRequestId, withRequestId } from "./command";

describe("newRequestId", () => {
  it("generates a valid UUID v4 string", () => {
    const id = newRequestId();
    expect(id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
    );
  });

  it("generates unique IDs across calls", () => {
    const id1 = newRequestId();
    const id2 = newRequestId();
    expect(id1).not.toBe(id2);
  });
});

describe("withRequestId", () => {
  it("attaches a new request_id if none provided", () => {
    const payload = { amount: 1000 };
    const wrapped = withRequestId(payload);
    expect(wrapped.amount).toBe(1000);
    expect(wrapped.request_id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
    );
  });

  it("reuses provided request_id if specified", () => {
    const fixedId = "11111111-1111-4111-8111-111111111111";
    const payload = { amount: 2000 };
    const wrapped = withRequestId(payload, fixedId);
    expect(wrapped.request_id).toBe(fixedId);
  });
});
