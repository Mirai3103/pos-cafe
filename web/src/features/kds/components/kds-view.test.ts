import { describe, expect, it } from "bun:test";
import { remakeRequestFor } from "./kds-view";

describe("remakeRequestFor", () => {
  it("maps a customer-request Waste to OTHER with a Vietnamese note", () => {
    expect(remakeRequestFor({ reason: "CUSTOMER_REQUEST" })).toEqual({
      reason: "OTHER",
      note: "Khách yêu cầu pha lại",
    });
  });

  it("preserves the original Waste note when mapping to OTHER", () => {
    expect(remakeRequestFor({ reason: "CUSTOMER_REQUEST", note: "Khách đổi ý" })).toEqual({
      reason: "OTHER",
      note: "Khách đổi ý",
    });
  });

  it("keeps a backend-supported Remake reason", () => {
    expect(remakeRequestFor({ reason: "QUALITY_FAILURE", note: "Ly bị nứt" })).toEqual({
      reason: "QUALITY_FAILURE",
      note: "Ly bị nứt",
    });
  });
});
