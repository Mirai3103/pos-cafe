import { describe, it, expect, mock, beforeEach } from "bun:test";
import { renderToString } from "react-dom/server";
import type {
  CatalogSellableCategoryResponse,
  SalesDraftItemResponse,
  SalesServiceSessionResponse,
} from "@/api/generated/models";
import type { ItemPickerConfig } from "./item-picker-dialog";

// Mock @tanstack/react-router
mock.module("@tanstack/react-router", () => ({
  Link: ({
    children,
    to,
    className,
  }: {
    children?: React.ReactNode;
    to?: string;
    className?: string;
  }) => (
    <a href={to} className={className}>
      {children}
    </a>
  ),
}));

// Mock hooks
let mockShiftState: { data?: { state?: string }; isLoading: boolean } = {
  data: { state: "OPEN" },
  isLoading: false,
};

let mockMenuState: {
  data?: { categories?: CatalogSellableCategoryResponse[] };
  isLoading: boolean;
  isError: boolean;
  error?: unknown;
  refetch: () => void;
} = {
  data: { categories: [] },
  isLoading: false,
  isError: false,
  refetch: () => {},
};

let mockSessionState: {
  data?: SalesServiceSessionResponse | null;
  isLoading: boolean;
  isError: boolean;
  refetch: () => void;
} = {
  data: null,
  isLoading: false,
  isError: false,
  refetch: () => {},
};

mock.module("@/features/shift/api/use-shift", () => ({
  useCurrentShift: () => mockShiftState,
}));

mock.module("../api/use-pos", () => ({
  useSellableMenu: () => mockMenuState,
  useServiceSession: () => mockSessionState,
  useStartTakeawaySession: () => ({
    startTakeaway: async () => ({ id: "mock-session-id" }),
  }),
  useAddDraftItem: () => ({
    addDraftItem: async () => ({}),
    isPending: false,
  }),
  useUpdateDraftItemQuantity: () => ({
    updateQuantity: async () => ({}),
  }),
  useUpdateDraftItemSize: () => ({
    updateSize: async () => ({}),
  }),
  useUpdateDraftItemModifiers: () => ({
    updateModifiers: async () => ({}),
  }),
  useUpdateDraftItemPreparationNote: () => ({
    updatePreparationNote: async () => ({}),
  }),
  useRemoveDraftItem: () => ({
    removeDraftItem: async () => ({}),
  }),
}));

mock.module("../api/use-checkout", () => ({
  useCommitDraft: () => ({
    commitDraft: async () => ({}),
  }),
  usePayCash: () => ({
    payCash: async () => ({}),
  }),
  // PosView drives checkout through this hook, so stubbing it is what keeps
  // the render tests off the real mutation hooks.
  useCheckoutFlow: () => ({
    isPaymentOpen: false,
    isPaying: false,
    paymentError: null,
    changeDueVnd: null,
    openPaymentDialog: () => {},
    closePaymentDialog: () => {},
    confirmPayment: async () => {},
    finishPayment: () => {},
    nextCustomer: () => {},
    submitStatus: "idle",
    submitError: null,
    submitOrder: async () => {},
  }),
}));

import { PosView } from "./pos-view";
import { matchesDraftItemConfig } from "../utils/selection";

describe("pos-view coordinator", () => {
  beforeEach(() => {
    mockShiftState = {
      data: { state: "OPEN" },
      isLoading: false,
    };
    mockMenuState = {
      data: { categories: [] },
      isLoading: false,
      isError: false,
      refetch: () => {},
    };
    mockSessionState = {
      data: null,
      isLoading: false,
      isError: false,
      refetch: () => {},
    };
  });

  describe("exports", () => {
    it("exports PosView component function", () => {
      expect(typeof PosView).toBe("function");
    });
  });

  describe("matchesDraftItemConfig", () => {
    const baseDraftItem: SalesDraftItemResponse = {
      id: "draft-1",
      menu_item_id: "item-cf-1",
      size_id: "size-m",
      preparation_note: "it duong",
      selected_modifier_options: [
        { id: "opt-1", name: "Tran chau" },
        { id: "opt-2", name: "Thach" },
      ],
    };

    const baseConfig: ItemPickerConfig = {
      sizeId: "size-m",
      preparationNote: "it duong",
      selectedOptionIds: ["opt-1", "opt-2"],
      quantity: 1,
    };

    it("returns true when menu item, size, note, and modifiers match exactly", () => {
      expect(
        matchesDraftItemConfig(baseDraftItem, "item-cf-1", baseConfig),
      ).toBe(true);
    });

    it("returns true regardless of modifier option array order", () => {
      const reversedConfig: ItemPickerConfig = {
        ...baseConfig,
        selectedOptionIds: ["opt-2", "opt-1"],
      };
      expect(
        matchesDraftItemConfig(baseDraftItem, "item-cf-1", reversedConfig),
      ).toBe(true);
    });

    it("returns false when menu_item_id differs", () => {
      expect(
        matchesDraftItemConfig(baseDraftItem, "item-other", baseConfig),
      ).toBe(false);
    });

    it("returns false when size_id differs", () => {
      const differentSize: ItemPickerConfig = {
        ...baseConfig,
        sizeId: "size-l",
      };
      expect(
        matchesDraftItemConfig(baseDraftItem, "item-cf-1", differentSize),
      ).toBe(false);
    });

    it("returns false when preparation_note differs", () => {
      const differentNote: ItemPickerConfig = {
        ...baseConfig,
        preparationNote: "nhieu da",
      };
      expect(
        matchesDraftItemConfig(baseDraftItem, "item-cf-1", differentNote),
      ).toBe(false);
    });

    it("returns false when modifier option count or IDs differ", () => {
      const fewerModifiers: ItemPickerConfig = {
        ...baseConfig,
        selectedOptionIds: ["opt-1"],
      };
      expect(
        matchesDraftItemConfig(baseDraftItem, "item-cf-1", fewerModifiers),
      ).toBe(false);

      const differentModifiers: ItemPickerConfig = {
        ...baseConfig,
        selectedOptionIds: ["opt-1", "opt-3"],
      };
      expect(
        matchesDraftItemConfig(baseDraftItem, "item-cf-1", differentModifiers),
      ).toBe(false);
    });

    it("handles items with undefined sizes, notes, and modifier options", () => {
      const simpleDraftItem: SalesDraftItemResponse = {
        id: "draft-2",
        menu_item_id: "item-simple",
      };
      const simpleConfig: ItemPickerConfig = {
        selectedOptionIds: [],
        preparationNote: "",
        quantity: 1,
      };

      expect(
        matchesDraftItemConfig(simpleDraftItem, "item-simple", simpleConfig),
      ).toBe(true);
    });
  });

  describe("rendering states", () => {
    it("renders loading state when shift query is loading", () => {
      mockShiftState.isLoading = true;
      const html = renderToString(<PosView />);
      expect(html).toContain("Đang tải thực đơn bán hàng...");
    });

    it("renders loading state when menu query is loading", () => {
      mockMenuState.isLoading = true;
      const html = renderToString(<PosView />);
      expect(html).toContain("Đang tải thực đơn bán hàng...");
    });

    it("renders error state with retry button when menu query fails", () => {
      mockMenuState.isError = true;
      mockMenuState.error = new Error("Network timeout");
      const html = renderToString(<PosView />);
      expect(html).toContain("Không thể tải thực đơn");
      expect(html).toContain("Thử lại");
    });

    it("renders closed shift state with NoShiftNotice in draft panel when shift is not open", () => {
      mockShiftState.data = { state: "CLOSED" };
      const html = renderToString(<PosView />);
      expect(html).toContain("Chưa có ca bán hàng mở");
      expect(html).toContain("Mở ca làm việc");
    });

    it("renders open shift terminal with menu categories and empty draft state", () => {
      mockShiftState.data = { state: "OPEN" };
      mockMenuState.data = {
        categories: [
          {
            id: "cat-1",
            name: "Cà phê",
            items: [
              {
                id: "item-cf-1",
                name: "Cà phê sữa đá",
                price_vnd: 29000,
              },
            ],
          },
        ],
      };

      const html = renderToString(<PosView />);
      expect(html).toContain("Cà phê");
      expect(html).toContain("Cà phê sữa đá");
      expect(html).toContain("29.000");
      expect(html).toContain("Chưa có món nào trong đơn");
    });

    it("renders open shift terminal with active session items in draft panel", () => {
      mockShiftState.data = { state: "OPEN" };
      mockMenuState.data = {
        categories: [
          {
            id: "cat-1",
            name: "Cà phê",
            items: [
              {
                id: "item-cf-1",
                name: "Cà phê sữa đá",
                price_vnd: 30000,
              },
            ],
          },
        ],
      };
      mockSessionState.data = {
        id: "session-active-1",
        service_number: "088",
        service_mode: "takeaway",
        state: "ACTIVE",
        draft: {
          items: [
            {
              id: "draft-item-1",
              menu_item_id: "item-cf-1",
              name: "Cà phê sữa đá",
              price_vnd: 30000,
              quantity: 2,
              available: true,
            },
          ],
        },
      };

      const html = renderToString(<PosView />);
      expect(html).toContain("#088");
      expect(html).toContain("60.000"); // 30000 * 2 line total and subtotal
      expect(html).toContain("Cà phê sữa đá");
    });

    it("swaps the draft panel for the check panel once the draft is committed", () => {
      mockShiftState.data = { state: "OPEN" };
      mockSessionState.data = {
        id: "session-committed-1",
        service_number: "091",
        service_mode: "takeaway",
        state: "ACTIVE",
        checks: [
          {
            id: "check-1",
            state: "OPEN",
            charge_vnd: 47000,
            balance_vnd: 47000,
            created_at: "2026-09-24T01:00:00Z",
            payments: [],
            allocations: [
              {
                id: "alloc-1",
                name: "Cà phê sữa đá",
                allocated_quantity: 2,
                amount_vnd: 47000,
                modifiers: [],
              },
            ],
          },
        ],
      };

      const html = renderToString(<PosView />);
      expect(html).toContain("Còn phải thu");
      expect(html).toContain("Thu tiền (F9)");
      expect(html).toContain("47.000");
      // The editable-draft affordances are gone.
      expect(html).not.toContain("Thanh toán (F9)");
      expect(html).not.toContain("Tạm tính");
    });
  });
});
