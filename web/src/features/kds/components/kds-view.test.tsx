import { describe, expect, it, mock } from "bun:test";
import { renderToString } from "react-dom/server";
import type { PreparationQueueResponse } from "@/api/generated/models";

const queueResult: {
  data: PreparationQueueResponse | undefined;
  isError: boolean;
  isLoading: boolean;
  refetch: () => void;
} = {
  data: undefined,
  isError: false,
  isLoading: false,
  refetch: () => {},
};

const actionsResult = {
  isPending: false,
  advanceUnit: async () => ({}),
  advanceMany: async () => [],
  wasteUnit: async () => ({}),
  remakeWaste: async () => ({}),
  correctState: async () => [],
  acknowledgeAlert: async () => ({}),
};

mock.module("../api/use-preparation-queue", () => ({
  usePreparationQueue: () => queueResult,
}));

mock.module("../api/use-preparation-actions", () => ({
  usePreparationActions: () => actionsResult,
}));

import { KdsView, remakeRequestFor } from "./kds-view";

const queueEnvelope: PreparationQueueResponse = {
  observed_at: "2026-09-26T01:03:00Z",
  alerts: [],
  corrections: [],
  units: [
    {
      id: "u1",
      item_name: "Bạc xỉu đá",
      state: "QUEUED",
      unit_number: 1,
      service_number: "014",
      queued_at: "2026-09-26T01:00:00Z",
      category_name: "Đồ uống",
    },
  ],
};

describe("KdsView", () => {
  it("keeps rendering the board when a poll fails after data has loaded", () => {
    queueResult.data = queueEnvelope;
    queueResult.isError = true;
    queueResult.isLoading = false;

    const html = renderToString(<KdsView />);

    expect(html).toContain("Chờ pha");
    expect(html).toContain("Đang pha");
    expect(html).toContain("Đã xong");
    expect(html).toContain("014");
    expect(html).toContain("Mất kết nối tới máy chủ");
    expect(html).not.toContain("Không tải được hàng chờ pha chế");
  });

  it("still shows the full-screen failure on a genuine first-load error", () => {
    queueResult.data = undefined;
    queueResult.isError = true;
    queueResult.isLoading = false;

    expect(renderToString(<KdsView />)).toContain("Không tải được hàng chờ pha chế");
  });
});

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
