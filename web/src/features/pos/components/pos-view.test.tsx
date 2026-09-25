import { describe, it, expect, mock, beforeEach } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
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
  // PosView calls useNavigate unconditionally (leaving the floor / dismissing
  // a dine-in sale); a no-op stub keeps the render tests off the real router.
  useNavigate: () => () => {},
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
  useActiveSessions: () => ({
    data: [],
    isLoading: false,
    isError: false,
    error: null,
    dataUpdatedAt: 0,
    refetch: async () => ({}),
  }),
  useCompletedSale: () => ({ data: undefined, isError: false }),
  useStartNextDraft: () => ({
    startNextDraft: async () => ({}),
  }),
}));

mock.module("../api/use-checkout", () => ({
  useCommitDraft: () => ({
    commitDraft: async () => ({}),
  }),
  usePayCash: () => ({
    payCash: async () => ({}),
  }),
  // useDineInFlow (Task 7) imports these alongside the checkout hooks above,
  // and runs unmocked inside PosView, so this module's mock must cover its
  // full shape too, not just what useCheckoutFlow needs.
  useSubmitOrder: () => ({
    submitOrder: async () => ({}),
  }),
  SUBMIT_ALREADY_DONE_CODES: new Set(["NOTHING_TO_SUBMIT"]),
  COMMIT_FAILURE_CODES: new Set(["EMPTY_DRAFT"]),
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

mock.module("../api/use-close-session", () => ({
  useCloseFlow: () => ({
    isClosing: false,
    completedSale: null,
    closeSession: async () => {},
    dismissCompletedSale: () => {},
  }),
}));

import { PosView } from "./pos-view";
import { matchesDraftItemConfig } from "../utils/selection";

function renderPosView() {
  return renderToString(
    <QueryClientProvider client={new QueryClient()}>
      <PosView />
    </QueryClientProvider>,
  );
}

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
      const html = renderPosView();
      expect(html).toContain("Đang tải thực đơn bán hàng...");
    });

    it("renders loading state when menu query is loading", () => {
      mockMenuState.isLoading = true;
      const html = renderPosView();
      expect(html).toContain("Đang tải thực đơn bán hàng...");
    });

    it("renders error state with retry button when menu query fails", () => {
      mockMenuState.isError = true;
      mockMenuState.error = new Error("Network timeout");
      const html = renderPosView();
      expect(html).toContain("Không thể tải thực đơn");
      expect(html).toContain("Thử lại");
    });

    it("renders closed shift state with NoShiftNotice in draft panel when shift is not open", () => {
      mockShiftState.data = { state: "CLOSED" };
      const html = renderPosView();
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

      const html = renderPosView();
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

      const html = renderPosView();
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

      const html = renderPosView();
      expect(html).toContain("Còn phải thu");
      expect(html).toContain("Thu tiền (F9)");
      expect(html).toContain("47.000");
      // The editable-draft affordances are gone.
      expect(html).not.toContain("Thanh toán (F9)");
      expect(html).not.toContain("Tạm tính");
    });

    it("renders the DineInPanel (not CheckPanel/DraftPanel) and enables ordering when a DINE_IN session can order", () => {
      mockShiftState.data = { state: "OPEN" };
      mockMenuState.data = {
        categories: [
          {
            id: "cat-tra",
            name: "Trà",
            items: [
              {
                id: "item-tra-1",
                name: "Trà đào cam sả",
                price_vnd: 39000,
              },
            ],
          },
        ],
      };
      mockSessionState.data = {
        id: "session-dinein-1",
        service_number: "201",
        service_mode: "DINE_IN",
        state: "ACTIVE",
        tables: [{ id: "t1", name: "Bàn 5" }],
        checks: [
          {
            id: "check-dinein-1",
            state: "OPEN",
            charge_vnd: 55000,
            balance_vnd: 55000,
            payments: [],
            allocations: [
              {
                id: "alloc-dinein-1",
                name: "Trà đào cam sả",
                allocated_quantity: 1,
                amount_vnd: 55000,
                submitted: true,
              },
            ],
          },
        ],
        orders: [{ id: "order-dinein-1" }],
        preparation_units: [{ id: "unit-dinein-1", state: "FULFILLED" }],
      };

      const html = renderPosView();

      // DineInPanel-only chrome: DineInHeader's change-tables button and
      // DineInActions' leave-to-floor button. Neither CheckPanel nor
      // DraftPanel ever renders these strings.
      expect(html).toContain("Đổi bàn");
      expect(html).toContain("Về sơ đồ bàn");
      expect(html).toContain("Khách dùng tại bàn");
      // The open Check has no draft in progress, so DineInActions offers
      // collecting, with F9 bound to it (no unsubmitted round to send first).
      expect(html).toContain("Thu tiền (F9)");

      // The takeaway-only chrome from CheckPanel/DraftPanel must not appear.
      expect(html).not.toContain("Đơn mang đi");
      expect(html).not.toContain("Thanh toán (F9)");

      // MenuGrid: canOrder is true and the shift is open, so the item card
      // renders enabled (no disabled styling on the card).
      expect(html).toContain("Trà đào cam sả");
      expect(html).not.toContain("opacity-50 cursor-not-allowed");
    });

    it("disables MenuGrid ordering and offers Gửi bếp when a DINE_IN round is committed but unsubmitted", () => {
      mockShiftState.data = { state: "OPEN" };
      mockMenuState.data = {
        categories: [
          {
            id: "cat-tra",
            name: "Trà",
            items: [
              {
                id: "item-tra-1",
                name: "Trà đào cam sả",
                price_vnd: 39000,
              },
            ],
          },
        ],
      };
      mockSessionState.data = {
        id: "session-dinein-2",
        service_number: "202",
        service_mode: "DINE_IN",
        state: "ACTIVE",
        tables: [{ id: "t2", name: "Bàn 7" }],
        checks: [
          {
            id: "check-dinein-2",
            state: "OPEN",
            charge_vnd: 40000,
            balance_vnd: 40000,
            payments: [],
            allocations: [
              {
                id: "alloc-dinein-2",
                name: "Cà phê đen",
                allocated_quantity: 1,
                amount_vnd: 40000,
                // Committed to the Check but not yet sent to the bar: this is
                // what makes deriveDineInStatus().canOrder false.
                submitted: false,
              },
            ],
          },
        ],
        orders: [{ id: "order-dinein-2" }],
        preparation_units: [],
      };

      const html = renderPosView();

      // DineInPanel warns that ordering is blocked until the round is sent...
      expect(html).toContain("Gửi bếp lượt trước để gọi thêm");
      // ...and DineInActions' F9 binding goes to sending, not collecting.
      expect(html).toContain("Gửi bếp (F9)");

      // MenuGrid: canOrder is false (dineInStatus gates disabled here, not
      // phase), so the item card renders with its disabled styling.
      expect(html).toContain("Trà đào cam sả");
      expect(html).toContain("opacity-50 cursor-not-allowed");
    });
  });
});
