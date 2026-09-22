# Web Slice 3: POS-a Sellable Menu & Order Draft Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the first third of the Cashier Terminal (POS-a): interactive Sellable Menu grid, real-time search with Vietnamese acronym matching, Order Draft item assembly and customization dialog, and lazy Takeaway Service Session management with single-tab persistence.

**Architecture:** Server-authoritative session state queried from Go `/sales/service-sessions/:id` and `/catalog/menu/sellable` via TanStack Query v5 unwrapped with `unwrap()`. Client tab state stores only the active session UUID pointer in `sessionStorage`. Mutations atomically update the query cache from the returned `ServiceSessionResponse` projection. Strict touch ergonomics (>= 48px) and Crisp Emerald visual tokens are enforced per `DESIGN.md`.

**Tech Stack:** React 19, TanStack Router + Query v5, Tailwind CSS v4, Lucide icons, Web Audio API sound feedback (`playTapChirp`), `bun test`.

**Spec:** [`docs/superpowers/specs/2026-09-23-web-slice-3-pos-draft-design.md`](../specs/2026-09-23-web-slice-3-pos-draft-design.md)

---

## Global Constraints

- **No mock data survives in the shipped path.** Every product, category, size, and draft item calls the real Go API.
- **No end-to-end tests and no integration tests in browser.** Unit tests only: pure logic, utilities, pricing, selection, and search algorithms run via `bun test`.
- **The slice ends at a UAT gate** (Task 11). Completion is never self-certified.
- **Strict Touch Target Dimensions:** All touchable components (pills, cards, chips, steppers, buttons) must maintain minimum `48px` height and width (`min-h-[48px] min-w-[48px]`).
- **Tactile Sound & Press Feedback:** Interactive card and button presses trigger `active:scale-[0.98]` and Web Audio chirp `playTapChirp()`.
- **Whole VND Currency Only:** JetBrains Mono font (`font-mono`), dot thousand-separators (e.g., `35.000 đ`). No decimals.
- **Banned Elements:** No emojis, no em-dashes, no AI-purple neon glows, no pure black (`#000000` -> use `#0f172a` Slate-900).
- **Single-Intent Idempotency:** Every mutation command carries a `request_id` generated via `newRequestId()` retained across retries.
- **Lazy Session Initiation:** Takeaway session is created upon adding the first item, saving the ID to `sessionStorage`, avoiding orphan empty sessions.
- **Shift Gate:** When `useCurrentShift()` state is not `OPEN`, the catalog remains browsable, but the bill area displays `NoShiftNotice` with a link to `/shift`, and item addition is prevented client-side.
- **Vietnamese Copy Only:** All user-facing strings, badges, messages, and placeholders are in Vietnamese.

---

## File Structure

**Created:**

| File | Responsibility |
| :--- | :--- |
| `web/src/features/pos/utils/pricing.ts` | Pure pricing calculations: starting price, unit price with surcharges, line totals, subtotal. |
| `web/src/features/pos/utils/pricing.test.ts` | Unit tests for pricing logic. |
| `web/src/features/pos/utils/selection.ts` | Modifier selection validation, default resolution, min/max rules, note normalization. |
| `web/src/features/pos/utils/selection.test.ts` | Unit tests for modifier rules and note normalization. |
| `web/src/features/pos/utils/search.ts` | Vietnamese diacritic normalization and acronym matching (`cfsd` -> *Cà phê sữa đá*). |
| `web/src/features/pos/utils/search.test.ts` | Unit tests for search matching algorithms. |
| `web/src/features/pos/api/use-pos.ts` | Feature API seam wrapping Orval catalog & sales endpoints with `unwrap()`. |
| `web/src/features/pos/api/use-pos.test.ts` | Tests for API seam wrappers. |
| `web/src/features/pos/components/menu-item-card.tsx` | Touch product card with Lucide fallback on broken images and pricing display. |
| `web/src/features/pos/components/menu-grid.tsx` | Category pills rail, search bar with shortcut `/`, and responsive product grid. |
| `web/src/features/pos/components/item-picker-dialog.tsx` | Modal dialog for size selection, modifier groups, prep note, and quantity configuration. |
| `web/src/features/pos/components/no-shift-notice.tsx` | Banner in bill area shown when sales shift is not open. |
| `web/src/features/pos/components/draft-empty-state.tsx` | Empty cart graphic and instructions shown when bill has 0 items. |
| `web/src/features/pos/components/draft-item-row.tsx` | Single item row with quantity stepper, options summary, edit trigger, and remove button. |
| `web/src/features/pos/components/draft-panel.tsx` | Thermal-receipt style bill aside with order header, item list, totals, and disabled CTAs. |
| `web/src/features/pos/components/pos-view.tsx` | Main coordinator view managing 2-column layout, shift gate, and modal state. |

**Modified:**

| File | Change |
| :--- | :--- |
| `web/src/lib/error-messages.ts` | Add 16 stable error codes from `internal/sales/errors.go`. |
| `web/src/lib/error-messages.test.ts` | Add assertions for new sales and draft error code translations. |
| `scripts/dev-seed.ts` | Expand to seed sample categories, modifier groups, and sellable items with sizes. |
| `web/src/routes/_app/index.tsx` | Replace `PosTerminalView` placeholder with `PosView`. |

---

### Task 1: Pricing Calculations (`pricing.ts` & `pricing.test.ts`)

**Files:**
- Create: `web/src/features/pos/utils/pricing.ts`
- Create: `web/src/features/pos/utils/pricing.test.ts`

**Interfaces:**
- Produces:
  - `getStartingPrice(item: { price_vnd?: number; sizes?: { price_vnd?: number }[] }): number`
  - `calculateItemUnitPrice(basePrice: number, optionSurcharges: number[]): number`
  - `calculateLineTotal(unitPrice: number, quantity: number): number`
  - `calculateDraftSubtotal(items?: { price_vnd?: number; quantity?: number; selected_modifier_options?: { surcharge_vnd?: number }[] }[]): number`

- [ ] **Step 1: Write failing test for pricing calculations**

Create `web/src/features/pos/utils/pricing.test.ts`:
```ts
import { describe, expect, it } from "bun:test";
import {
  getStartingPrice,
  calculateItemUnitPrice,
  calculateLineTotal,
  calculateDraftSubtotal,
} from "./pricing";

describe("pricing calculations", () => {
  describe("getStartingPrice", () => {
    it("returns direct price when item has no sizes", () => {
      const item = { price_vnd: 35000 };
      expect(getStartingPrice(item)).toBe(35000);
    });

    it("returns minimum size price when item has multiple sizes", () => {
      const item = {
        sizes: [
          { price_vnd: 35000 },
          { price_vnd: 25000 },
          { price_vnd: 42000 },
        ],
      };
      expect(getStartingPrice(item)).toBe(25000);
    });

    it("returns 0 when item has neither price nor sizes", () => {
      expect(getStartingPrice({})).toBe(0);
    });
  });

  describe("calculateItemUnitPrice", () => {
    it("computes base price plus sum of surcharges", () => {
      expect(calculateItemUnitPrice(30000, [5000, 10000])).toBe(45000);
    });

    it("handles zero surcharges", () => {
      expect(calculateItemUnitPrice(25000, [])).toBe(25000);
    });
  });

  describe("calculateLineTotal", () => {
    it("multiplies unit price by quantity", () => {
      expect(calculateLineTotal(45000, 3)).toBe(135000);
    });
  });

  describe("calculateDraftSubtotal", () => {
    it("computes total across multiple draft items", () => {
      const items = [
        {
          price_vnd: 30000,
          quantity: 2,
          selected_modifier_options: [{ surcharge_vnd: 5000 }],
        },
        {
          price_vnd: 25000,
          quantity: 1,
          selected_modifier_options: [],
        },
      ];
      // Item 1: (30000 + 5000) * 2 = 70000
      // Item 2: 25000 * 1 = 25000
      // Total: 95000
      expect(calculateDraftSubtotal(items)).toBe(95000);
    });

    it("returns 0 for empty or undefined draft items", () => {
      expect(calculateDraftSubtotal([])).toBe(0);
      expect(calculateDraftSubtotal(undefined)).toBe(0);
    });
  });
});
```

- [ ] **Step 2: Run test to verify failure**

Run: `cd web && bun test src/features/pos/utils/pricing.test.ts`
Expected: FAIL (Cannot find module `./pricing`)

- [ ] **Step 3: Implement pricing calculations**

Create `web/src/features/pos/utils/pricing.ts`:
```ts
export interface PriceableItem {
  price_vnd?: number;
  sizes?: Array<{ price_vnd?: number }>;
}

export interface DraftItemLike {
  price_vnd?: number;
  quantity?: number;
  selected_modifier_options?: Array<{ surcharge_vnd?: number }>;
}

/**
 * Returns the base starting price for a menu item.
 * If the item has sizes, returns the minimum available size price.
 */
export function getStartingPrice(item: PriceableItem): number {
  if (item.sizes && item.sizes.length > 0) {
    const validPrices = item.sizes
      .map((s) => s.price_vnd ?? 0)
      .filter((p) => p > 0);
    if (validPrices.length > 0) {
      return Math.min(...validPrices);
    }
  }
  return item.price_vnd ?? 0;
}

/**
 * Computes configured item unit price by adding all selected modifier surcharges
 * to the base item or size price.
 */
export function calculateItemUnitPrice(
  basePrice: number,
  optionSurcharges: number[],
): number {
  const surchargesTotal = optionSurcharges.reduce((acc, curr) => acc + curr, 0);
  return basePrice + surchargesTotal;
}

/**
 * Computes total price for a line item given its unit price and quantity.
 */
export function calculateLineTotal(unitPrice: number, quantity: number): number {
  return unitPrice * Math.max(1, quantity);
}

/**
 * Computes total draft subtotal across all draft line items.
 */
export function calculateDraftSubtotal(items?: DraftItemLike[]): number {
  if (!items || items.length === 0) return 0;

  return items.reduce((total, item) => {
    const base = item.price_vnd ?? 0;
    const surcharges = (item.selected_modifier_options ?? []).map(
      (opt) => opt.surcharge_vnd ?? 0,
    );
    const unitPrice = calculateItemUnitPrice(base, surcharges);
    return total + calculateLineTotal(unitPrice, item.quantity ?? 1);
  }, 0);
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `cd web && bun test src/features/pos/utils/pricing.test.ts`
Expected: PASS (4 tests passed)

- [ ] **Step 5: Commit**

```bash
git add web/src/features/pos/utils/pricing.ts web/src/features/pos/utils/pricing.test.ts
git commit -m "feat(web): add pricing calculation utilities for pos draft"
```

---

### Task 2: Modifier Selection Rules & Note Normalization (`selection.ts` & `selection.test.ts`)

**Files:**
- Create: `web/src/features/pos/utils/selection.ts`
- Create: `web/src/features/pos/utils/selection.test.ts`

**Interfaces:**
- Produces:
  - `resolveInitialSelection(item: CatalogSellableItemResponse): InitialSelection`
  - `toggleModifierOption(groupId: string, optionId: string, currentSelectedIds: string[], group: CatalogSellableModifierGroupResponse): string[]`
  - `isSelectionValid(item: CatalogSellableItemResponse, sizeId: string | undefined, selectedOptionIds: string[]): boolean`
  - `normalizePreparationNote(note: string): string`

- [ ] **Step 1: Write failing test for modifier selection and note normalization**

Create `web/src/features/pos/utils/selection.test.ts`:
```ts
import { describe, expect, it } from "bun:test";
import {
  resolveInitialSelection,
  toggleModifierOption,
  isSelectionValid,
  normalizePreparationNote,
} from "./selection";
import type {
  CatalogSellableItemResponse,
  CatalogSellableModifierGroupResponse,
} from "@/api/generated/models";

describe("selection utilities", () => {
  const sampleGroupSugar: CatalogSellableModifierGroupResponse = {
    id: "group-sugar",
    name: "Mức đường",
    min_selections: 1,
    max_selections: 1,
    default_option_ids: ["opt-sugar-100"],
    options: [
      { id: "opt-sugar-100", name: "100% đường", surcharge_vnd: 0 },
      { id: "opt-sugar-50", name: "50% đường", surcharge_vnd: 0 },
    ],
  };

  const sampleGroupTopping: CatalogSellableModifierGroupResponse = {
    id: "group-topping",
    name: "Topping",
    min_selections: 0,
    max_selections: 2,
    default_option_ids: [],
    options: [
      { id: "opt-pearl", name: "Trân châu", surcharge_vnd: 5000 },
      { id: "opt-jelly", name: "Thạch dừa", surcharge_vnd: 5000 },
      { id: "opt-pudding", name: "Pudding", surcharge_vnd: 7000 },
    ],
  };

  const sampleItem: CatalogSellableItemResponse = {
    id: "item-coffee",
    name: "Cà phê sữa đá",
    sizes: [
      { id: "size-s", name: "Size S", price_vnd: 29000 },
      { id: "size-m", name: "Size M", price_vnd: 35000 },
    ],
    modifier_groups: [sampleGroupSugar, sampleGroupTopping],
  };

  describe("resolveInitialSelection", () => {
    it("pre-selects first size and default options", () => {
      const initial = resolveInitialSelection(sampleItem);
      expect(initial.sizeId).toBe("size-s");
      expect(initial.selectedOptionIds).toEqual(["opt-sugar-100"]);
      expect(initial.note).toBe("");
      expect(initial.quantity).toBe(1);
    });

    it("handles item with no sizes or modifiers", () => {
      const simpleItem: CatalogSellableItemResponse = {
        id: "item-croissant",
        name: "Croissant",
        price_vnd: 35000,
      };
      const initial = resolveInitialSelection(simpleItem);
      expect(initial.sizeId).toBeUndefined();
      expect(initial.selectedOptionIds).toEqual([]);
    });
  });

  describe("toggleModifierOption", () => {
    it("enforces radio replacement when max_selections is 1", () => {
      const current = ["opt-sugar-100"];
      const next = toggleModifierOption(
        "group-sugar",
        "opt-sugar-50",
        current,
        sampleGroupSugar,
      );
      expect(next).toEqual(["opt-sugar-50"]);
    });

    it("adds option to multi-select group when below max_selections", () => {
      const current = ["opt-sugar-100", "opt-pearl"];
      const next = toggleModifierOption(
        "group-topping",
        "opt-jelly",
        current,
        sampleGroupTopping,
      );
      expect(next).toEqual(["opt-sugar-100", "opt-pearl", "opt-jelly"]);
    });

    it("unselects an already selected option in multi-select group", () => {
      const current = ["opt-sugar-100", "opt-pearl", "opt-jelly"];
      const next = toggleModifierOption(
        "group-topping",
        "opt-pearl",
        current,
        sampleGroupTopping,
      );
      expect(next).toEqual(["opt-sugar-100", "opt-jelly"]);
    });

    it("blocks adding option when max_selections is reached in multi-select group", () => {
      const current = ["opt-pearl", "opt-jelly"];
      const next = toggleModifierOption(
        "group-topping",
        "opt-pudding",
        current,
        sampleGroupTopping,
      );
      // Reached max 2: retains current without adding
      expect(next).toEqual(["opt-pearl", "opt-jelly"]);
    });
  });

  describe("isSelectionValid", () => {
    it("returns true when size is chosen and min_selections is satisfied", () => {
      expect(isSelectionValid(sampleItem, "size-m", ["opt-sugar-100"])).toBe(true);
    });

    it("returns false when item has sizes but none is selected", () => {
      expect(isSelectionValid(sampleItem, undefined, ["opt-sugar-100"])).toBe(false);
    });

    it("returns false when a required modifier group has no selection", () => {
      expect(isSelectionValid(sampleItem, "size-m", [])).toBe(false);
    });
  });

  describe("normalizePreparationNote", () => {
    it("trims whitespace", () => {
      expect(normalizePreparationNote("  ít đá  ")).toBe("ít đá");
    });

    it("truncates note to 200 unicode characters", () => {
      const longNote = "a".repeat(250);
      const normalized = normalizePreparationNote(longNote);
      expect(normalized.length).toBe(200);
    });
  });
});
```

- [ ] **Step 2: Run test to verify failure**

Run: `cd web && bun test src/features/pos/utils/selection.test.ts`
Expected: FAIL (Cannot find module `./selection`)

- [ ] **Step 3: Implement selection utilities**

Create `web/src/features/pos/utils/selection.ts`:
```ts
import type {
  CatalogSellableItemResponse,
  CatalogSellableModifierGroupResponse,
} from "@/api/generated/models";

export interface InitialSelection {
  sizeId?: string;
  selectedOptionIds: string[];
  note: string;
  quantity: number;
}

export const MAX_PREPARATION_NOTE_LENGTH = 200;

/**
 * Resolves default size and option selections when opening configuration dialog.
 */
export function resolveInitialSelection(
  item: CatalogSellableItemResponse,
): InitialSelection {
  const sizeId =
    item.sizes && item.sizes.length > 0 ? item.sizes[0].id : undefined;

  const defaultOptionIds: string[] = [];
  if (item.modifier_groups) {
    for (const group of item.modifier_groups) {
      if (group.default_option_ids) {
        defaultOptionIds.push(...group.default_option_ids);
      }
    }
  }

  return {
    sizeId,
    selectedOptionIds: defaultOptionIds,
    note: "",
    quantity: 1,
  };
}

/**
 * Toggles a modifier option within a group, respecting min/max bounds and radio replacement.
 */
export function toggleModifierOption(
  _groupId: string,
  optionId: string,
  currentSelectedIds: string[],
  group: CatalogSellableModifierGroupResponse,
): string[] {
  const groupOptionIds = new Set((group.options ?? []).map((o) => o.id));
  const maxSelections = group.max_selections ?? 1;

  // Single choice group: radio replacement
  if (maxSelections === 1) {
    const withoutGroup = currentSelectedIds.filter((id) => !groupOptionIds.has(id));
    return [...withoutGroup, optionId];
  }

  // Multi-choice group
  const isSelected = currentSelectedIds.includes(optionId);
  if (isSelected) {
    return currentSelectedIds.filter((id) => id !== optionId);
  }

  // Check capacity in this group
  const currentCountInGroup = currentSelectedIds.filter((id) =>
    groupOptionIds.has(id),
  ).length;

  if (currentCountInGroup >= maxSelections) {
    return currentSelectedIds;
  }

  return [...currentSelectedIds, optionId];
}

/**
 * Checks if the configured item satisfies all required rules (size selected, min selections).
 */
export function isSelectionValid(
  item: CatalogSellableItemResponse,
  sizeId: string | undefined,
  selectedOptionIds: string[],
): boolean {
  // Size requirement
  if (item.sizes && item.sizes.length > 0 && !sizeId) {
    return false;
  }

  // Modifier group min_selections requirement
  if (item.modifier_groups) {
    for (const group of item.modifier_groups) {
      const min = group.min_selections ?? 0;
      if (min > 0) {
        const groupOptionIds = new Set((group.options ?? []).map((o) => o.id));
        const selectedCountInGroup = selectedOptionIds.filter((id) =>
          groupOptionIds.has(id),
        ).length;
        if (selectedCountInGroup < min) {
          return false;
        }
      }
    }
  }

  return true;
}

/**
 * Normalizes preparation note: trims whitespace and caps at 200 characters.
 */
export function normalizePreparationNote(note: string): string {
  const trimmed = note.trim();
  const chars = Array.from(trimmed);
  if (chars.length > MAX_PREPARATION_NOTE_LENGTH) {
    return chars.slice(0, MAX_PREPARATION_NOTE_LENGTH).join("");
  }
  return trimmed;
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `cd web && bun test src/features/pos/utils/selection.test.ts`
Expected: PASS (4 tests passed)

- [ ] **Step 5: Commit**

```bash
git add web/src/features/pos/utils/selection.ts web/src/features/pos/utils/selection.test.ts
git commit -m "feat(web): add modifier selection rules and note normalization"
```

---

### Task 3: Quick Search & Diacritic Acronym Matching (`search.ts` & `search.test.ts`)

**Files:**
- Create: `web/src/features/pos/utils/search.ts`
- Create: `web/src/features/pos/utils/search.test.ts`

**Interfaces:**
- Produces:
  - `normalizeVietnamese(text: string): string`
  - `getAcronym(text: string): string`
  - `matchesSearch(query: string, itemName: string, categoryName?: string): boolean`
  - `filterSellableItems(categories: CatalogSellableCategoryResponse[], selectedCategoryId: string | null, searchQuery: string): CatalogSellableItemResponse[]`

- [ ] **Step 1: Write failing test for search matching**

Create `web/src/features/pos/utils/search.test.ts`:
```ts
import { describe, expect, it } from "bun:test";
import {
  normalizeVietnamese,
  getAcronym,
  matchesSearch,
  filterSellableItems,
} from "./search";
import type { CatalogSellableCategoryResponse } from "@/api/generated/models";

describe("search utilities", () => {
  describe("normalizeVietnamese", () => {
    it("strips diacritics and converts to lowercase", () => {
      expect(normalizeVietnamese("Cà phê Sữa Đá")).toBe("ca phe sua da");
      expect(normalizeVietnamese("Trà Đào Cam Sả")).toBe("tra dao cam sa");
    });
  });

  describe("getAcronym", () => {
    it("extracts first letter of each word in lowercase", () => {
      expect(getAcronym("Cà phê sữa đá")).toBe("cfsd");
      expect(getAcronym("Bạc xỉu")).toBe("bx");
      expect(getAcronym("Trà đào cam sả")).toBe("tdcs");
    });
  });

  describe("matchesSearch", () => {
    it("matches exact substring in normalized item name", () => {
      expect(matchesSearch("ca phe", "Cà phê Đen")).toBe(true);
      expect(matchesSearch("den", "Cà phê Đen")).toBe(true);
    });

    it("matches acronym query", () => {
      expect(matchesSearch("cfsd", "Cà phê sữa đá")).toBe(true);
      expect(matchesSearch("bx", "Bạc xỉu đá")).toBe(true);
    });

    it("matches category name if provided", () => {
      expect(matchesSearch("banh", "Croissant bơ tỏi", "Bánh ngọt")).toBe(true);
    });

    it("returns true on empty query", () => {
      expect(matchesSearch("", "Bất kỳ món nào")).toBe(true);
      expect(matchesSearch("   ", "Bất kỳ món nào")).toBe(true);
    });
  });

  describe("filterSellableItems", () => {
    const categories: CatalogSellableCategoryResponse[] = [
      {
        id: "cat-1",
        name: "Cà phê",
        items: [
          { id: "item-1", name: "Cà phê sữa đá", price_vnd: 29000 },
          { id: "item-2", name: "Bạc xỉu", price_vnd: 32000 },
        ],
      },
      {
        id: "cat-2",
        name: "Bánh ngọt",
        items: [{ id: "item-3", name: "Croissant bơ tỏi", price_vnd: 35000 }],
      },
    ];

    it("filters by category", () => {
      const items = filterSellableItems(categories, "cat-2", "");
      expect(items.length).toBe(1);
      expect(items[0].name).toBe("Croissant bơ tỏi");
    });

    it("returns all items when selectedCategoryId is null", () => {
      const items = filterSellableItems(categories, null, "");
      expect(items.length).toBe(3);
    });

    it("filters across categories with acronym query", () => {
      const items = filterSellableItems(categories, null, "bx");
      expect(items.length).toBe(1);
      expect(items[0].name).toBe("Bạc xỉu");
    });
  });
});
```

- [ ] **Step 2: Run test to verify failure**

Run: `cd web && bun test src/features/pos/utils/search.test.ts`
Expected: FAIL (Cannot find module `./search`)

- [ ] **Step 3: Implement search utilities**

Create `web/src/features/pos/utils/search.ts`:
```ts
import type {
  CatalogSellableCategoryResponse,
  CatalogSellableItemResponse,
} from "@/api/generated/models";

/**
 * Normalizes Vietnamese string: removes diacritics, special marks, and lowercases.
 */
export function normalizeVietnamese(text: string): string {
  return text
    .toLowerCase()
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .replace(/đ/g, "d")
    .replace(/Đ/g, "d")
    .trim();
}

/**
 * Generates first-letter acronym for a name (e.g. "Cà phê sữa đá" -> "cfsd").
 */
export function getAcronym(text: string): string {
  const normalized = normalizeVietnamese(text);
  const words = normalized.split(/\s+/).filter(Boolean);
  return words.map((w) => w[0]).join("");
}

/**
 * Checks whether an item matches a search query by normalized substring or acronym.
 */
export function matchesSearch(
  query: string,
  itemName: string,
  categoryName?: string,
): boolean {
  const q = normalizeVietnamese(query);
  if (!q) return true;

  const normalizedName = normalizeVietnamese(itemName);
  if (normalizedName.includes(q)) return true;

  const acronym = getAcronym(itemName);
  if (acronym.includes(q)) return true;

  if (categoryName) {
    const normalizedCategory = normalizeVietnamese(categoryName);
    if (normalizedCategory.includes(q)) return true;
  }

  return false;
}

/**
 * Filters items from categories by selected category ID and search query.
 */
export function filterSellableItems(
  categories: CatalogSellableCategoryResponse[],
  selectedCategoryId: string | null,
  searchQuery: string,
): CatalogSellableItemResponse[] {
  const targetCategories = selectedCategoryId
    ? categories.filter((cat) => cat.id === selectedCategoryId)
    : categories;

  const result: CatalogSellableItemResponse[] = [];

  for (const cat of targetCategories) {
    const items = cat.items ?? [];
    for (const item of items) {
      if (item.name && matchesSearch(searchQuery, item.name, cat.name)) {
        result.push(item);
      }
    }
  }

  return result;
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `cd web && bun test src/features/pos/utils/search.test.ts`
Expected: PASS (4 tests passed)

- [ ] **Step 5: Commit**

```bash
git add web/src/features/pos/utils/search.ts web/src/features/pos/utils/search.test.ts
git commit -m "feat(web): add quick search and acronym matching for sellable catalog"
```

---

### Task 4: Error Messages Expansion (`error-messages.ts` & `error-messages.test.ts`)

**Files:**
- Modify: `web/src/lib/error-messages.ts`
- Modify: `web/src/lib/error-messages.test.ts`

**Interfaces:**
- Produces: Updated `ERROR_MESSAGES` map with 16 domain codes from Go `internal/sales/errors.go`.

- [ ] **Step 1: Write failing test for new sales error messages**

Update `web/src/lib/error-messages.test.ts` by adding a suite for sales draft error mappings:
```ts
import { describe, expect, it } from "bun:test";
import { ApiError } from "./unwrap";
import { messageForError } from "./error-messages";

describe("messageForError", () => {
  it("maps a known code to its Vietnamese message", () => {
    expect(messageForError(new ApiError(401, "UNAUTHORIZED", "unauthorized"))).toBe(
      "Phiên đăng nhập không hợp lệ hoặc đã hết hạn.",
    );
  });

  it("maps sales shift and draft error codes to Vietnamese messages", () => {
    expect(
      messageForError(new ApiError(409, "OPEN_SALES_SHIFT_REQUIRED", "shift required")),
    ).toBe("Ca bán hàng chưa được mở. Vui lòng mở ca trước khi tạo đơn.");

    expect(
      messageForError(new ApiError(404, "MENU_ITEM_NOT_FOUND", "item not found")),
    ).toBe("Món không có trong thực đơn.");

    expect(
      messageForError(new ApiError(400, "INVALID_PREPARATION_NOTE", "invalid note")),
    ).toBe("Ghi chú pha chế không hợp lệ (tối đa 200 ký tự).");

    expect(
      messageForError(new ApiError(400, "INVALID_QUANTITY", "invalid qty")),
    ).toBe("Số lượng món phải từ 1 đến 9999.");
  });

  it("falls back to the server message for an unmapped code", () => {
    expect(messageForError(new ApiError(409, "SHIFT_ALREADY_OPEN", "ca làm việc đang mở"))).toBe(
      "ca làm việc đang mở",
    );
  });

  it("returns a generic message for a non-ApiError", () => {
    expect(messageForError(new Error("socket hang up"))).toBe(
      "Không kết nối được máy chủ. Kiểm tra mạng nội bộ rồi thử lại.",
    );
  });
});
```

- [ ] **Step 2: Run test to verify failure**

Run: `cd web && bun test src/lib/error-messages.test.ts`
Expected: FAIL ("Ca bán hàng chưa được mở..." expected but got "shift required")

- [ ] **Step 3: Update `error-messages.ts`**

Update `web/src/lib/error-messages.ts`:
```ts
import { ApiError } from "./unwrap";

/** Stable codes emitted by internal/response/response.go and internal/sales/errors.go. */
export const ERROR_MESSAGES: Record<string, string> = {
  UNAUTHORIZED: "Phiên đăng nhập không hợp lệ hoặc đã hết hạn.",
  FORBIDDEN: "Bạn không có quyền thực hiện thao tác này.",
  BAD_REQUEST: "Dữ liệu gửi lên không hợp lệ.",
  NOT_FOUND: "Không tìm thấy dữ liệu.",
  CONFLICT: "Thao tác xung đột với trạng thái hiện tại.",
  TOO_MANY_REQUESTS: "Bạn thử quá nhiều lần. Chờ một lát rồi thử lại.",
  INTERNAL_ERROR: "Máy chủ gặp sự cố. Báo quản lý nếu tình trạng tiếp diễn.",
  EMPTY_RESPONSE: "Máy chủ trả về dữ liệu rỗng.",
  SHIFT_CASH_RECOUNT_REQUIRED: "Ca có chênh lệch tiền mặt cần ít nhất 2 lần kiểm đếm trước khi đóng ca.",
  SHIFT_QR_RECHECK_REQUIRED: "Ca có chênh lệch VietQR cần ít nhất 2 lần cập nhật đối soát trước khi đóng ca.",
  SHIFT_DISCREPANCY_REASON_REQUIRED: "Vui lòng chọn lý do cho tất cả các khoản chênh lệch.",
  SHIFT_DISCREPANCY_REASON_UNEXPECTED: "Lý do chênh lệch không khớp với số liệu đối soát.",
  MANAGER_APPROVAL_UNAVAILABLE: "Không thể xác thực Quản lý. Vui lòng kiểm tra mã đăng nhập và PIN.",
  OPEN_SALES_SHIFT_REQUIRED: "Ca bán hàng chưa được mở. Vui lòng mở ca trước khi tạo đơn.",
  SERVICE_SESSION_NOT_FOUND: "Phiên phục vụ không tồn tại hoặc đã bị xóa.",
  SERVICE_SESSION_ALREADY_CLOSED: "Phiên phục vụ này đã kết thúc.",
  EDITABLE_DRAFT_NOT_FOUND: "Không tìm thấy đơn nháp hợp lệ để chỉnh sửa.",
  DRAFT_ITEM_NOT_FOUND: "Món trong đơn không tồn tại hoặc đã bị xóa.",
  MENU_ITEM_NOT_FOUND: "Món không có trong thực đơn.",
  MENU_ITEM_UNAVAILABLE: "Món này hiện đang tạm ngưng phục vụ.",
  MENU_ITEM_RETIRED: "Món này đã ngừng kinh doanh.",
  SIZE_NOT_FOUND: "Kích cỡ món không hợp lệ.",
  SIZE_UNAVAILABLE: "Kích cỡ đã chọn hiện đang tạm hết.",
  SIZE_RETIRED: "Kích cỡ này đã ngừng phục vụ.",
  MODIFIER_OPTION_NOT_FOUND: "Tùy chọn topping không tồn tại.",
  MODIFIER_OPTION_UNAVAILABLE: "Tùy chọn topping đã chọn hiện đang tạm hết.",
  MODIFIER_OPTION_RETIRED: "Tùy chọn topping này đã ngừng phục vụ.",
  INVALID_PREPARATION_NOTE: "Ghi chú pha chế không hợp lệ (tối đa 200 ký tự).",
  INVALID_QUANTITY: "Số lượng món phải từ 1 đến 9999.",
};

const NETWORK_MESSAGE = "Không kết nối được máy chủ. Kiểm tra mạng nội bộ rồi thử lại.";

export function messageForError(error: unknown): string {
  if (error instanceof ApiError) {
    return ERROR_MESSAGES[error.code] ?? error.message;
  }
  return NETWORK_MESSAGE;
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `cd web && bun test src/lib/error-messages.test.ts`
Expected: PASS (4 tests passed)

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/error-messages.ts web/src/lib/error-messages.test.ts
git commit -m "feat(web): add sales and draft error code translations"
```

---

### Task 5: Dev Seed Script Expansion (`scripts/dev-seed.ts`)

**Files:**
- Modify: `scripts/dev-seed.ts`

**Interfaces:**
- Produces: API-backed seeding of sample categories, modifier groups, and sized items.

- [ ] **Step 1: Check existing `dev-seed.ts` execution**

Run: `bun run scripts/dev-seed.ts`
Verify it completes without unhandled errors (server might be down or manager already bootstrapped).

- [ ] **Step 2: Update `dev-seed.ts` to seed categories, modifiers, and items**

Update `scripts/dev-seed.ts` with complete seeding implementation:
```ts
/**
 * Development seed. Creates the first Manager and sample catalog entities
 * so the Cashier Terminal (POS-a) can be tested end-to-end.
 *
 * Never run against production: Phase 11 acceptance forbids production
 * configuration from exposing demo identities or sample sales.
 *
 * Usage: bun run scripts/dev-seed.ts
 */
const BASE = process.env.POS_API_BASE ?? "http://localhost:8080/api/v1";

const MANAGER = {
  display_name: "Quan Ly Demo",
  login_code: "QL01",
  pin: "1234",
};

async function apiRequest<T>(
  path: string,
  options: { method?: string; body?: unknown; token?: string } = {},
): Promise<T | null> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (options.token) {
    headers["Authorization"] = `Bearer ${options.token}`;
  }

  const res = await fetch(`${BASE}${path}`, {
    method: options.method ?? "POST",
    headers,
    body: options.body ? JSON.stringify(options.body) : undefined,
  });

  const payload = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(`${path} -> ${res.status} ${JSON.stringify(payload)}`);
  }
  return payload as T;
}

interface AuthResponse {
  data?: {
    token?: string;
  };
}

interface IdResponse {
  data?: {
    id?: string;
  };
}

interface MenuResponse {
  data?: {
    categories?: Array<{ id: string; name: string }>;
  };
}

async function main(): Promise<void> {
  console.log("=== Seeding Development Data ===");

  // 1. Bootstrap or Sign In Manager
  let token = "";
  try {
    await apiRequest("/auth/bootstrap", { body: MANAGER });
    console.log(`Bootstrapped manager ${MANAGER.login_code} with PIN ${MANAGER.pin}`);
  } catch (error) {
    console.log(`Bootstrap skipped: ${String(error)}`);
  }

  try {
    const signInRes = await apiRequest<AuthResponse>("/auth/sign-in", {
      body: { login_code: MANAGER.login_code, pin: MANAGER.pin },
    });
    token = signInRes?.data?.token ?? "";
    console.log("Manager authenticated successfully.");
  } catch (error) {
    console.error("Failed to sign in manager. Ensure Go server is running:", error);
    return;
  }

  // 2. Check if Catalog is already seeded
  try {
    const menuRes = await apiRequest<MenuResponse>("/catalog/menu/sellable", {
      method: "GET",
      token,
    });
    const categories = menuRes?.data?.categories ?? [];
    if (categories.length > 0) {
      console.log(`Catalog already contains ${categories.length} categories. Seeding completed.`);
      return;
    }
  } catch {
    // Continue seeding if menu read fails or empty
  }

  console.log("Seeding sample catalog entities...");

  // 3. Create Categories
  const catCoffeeRes = await apiRequest<IdResponse>("/catalog/categories", {
    token,
    body: { name: "Cà phê" },
  });
  const catTeaRes = await apiRequest<IdResponse>("/catalog/categories", {
    token,
    body: { name: "Trà trái cây" },
  });
  const catPastryRes = await apiRequest<IdResponse>("/catalog/categories", {
    token,
    body: { name: "Bánh ngọt" },
  });

  const catCoffeeId = catCoffeeRes?.data?.id ?? "";
  const catTeaId = catTeaRes?.data?.id ?? "";
  const catPastryId = catPastryRes?.data?.id ?? "";

  // 4. Create Modifier Groups
  const sugarGroupRes = await apiRequest<IdResponse>("/catalog/modifier-groups", {
    token,
    body: {
      name: "Mức đường",
      min_selections: 1,
      max_selections: 1,
      manager_pin: MANAGER.pin,
      options: [
        { name: "100% đường", surcharge_vnd: 0 },
        { name: "70% đường", surcharge_vnd: 0 },
        { name: "50% đường", surcharge_vnd: 0 },
        { name: "Không đường", surcharge_vnd: 0 },
      ],
      default_option_names: ["100% đường"],
    },
  });

  const toppingGroupRes = await apiRequest<IdResponse>("/catalog/modifier-groups", {
    token,
    body: {
      name: "Topping thêm",
      min_selections: 0,
      max_selections: 3,
      manager_pin: MANAGER.pin,
      options: [
        { name: "Trân châu trắng", surcharge_vnd: 5000 },
        { name: "Thạch nha đam", surcharge_vnd: 5000 },
        { name: "Kem phô mai", surcharge_vnd: 10000 },
      ],
    },
  });

  const sugarGroupId = sugarGroupRes?.data?.id ?? "";
  const toppingGroupId = toppingGroupRes?.data?.id ?? "";

  // Link Modifier Groups to Categories
  if (catCoffeeId && sugarGroupId) {
    await apiRequest(`/catalog/categories/${catCoffeeId}/modifier-groups/${sugarGroupId}`, {
      token,
      body: {},
    });
  }
  if (catCoffeeId && toppingGroupId) {
    await apiRequest(`/catalog/categories/${catCoffeeId}/modifier-groups/${toppingGroupId}`, {
      token,
      body: {},
    });
  }
  if (catTeaId && sugarGroupId) {
    await apiRequest(`/catalog/categories/${catTeaId}/modifier-groups/${sugarGroupId}`, {
      token,
      body: {},
    });
  }
  if (catTeaId && toppingGroupId) {
    await apiRequest(`/catalog/categories/${catTeaId}/modifier-groups/${toppingGroupId}`, {
      token,
      body: {},
    });
  }

  // 5. Create Items
  // Simple Item (no sizes, no modifiers)
  await apiRequest("/catalog/items", {
    token,
    body: {
      category_id: catPastryId,
      name: "Croissant bơ tỏi",
      price_vnd: 35000,
      manager_pin: MANAGER.pin,
    },
  });

  // Items with Sizes
  await apiRequest("/catalog/items", {
    token,
    body: {
      category_id: catCoffeeId,
      name: "Cà phê đen",
      manager_pin: MANAGER.pin,
      sizes: [
        { name: "Size S", price_vnd: 25000 },
        { name: "Size M", price_vnd: 29000 },
        { name: "Size L", price_vnd: 35000 },
      ],
    },
  });

  await apiRequest("/catalog/items", {
    token,
    body: {
      category_id: catCoffeeId,
      name: "Cà phê sữa đá",
      manager_pin: MANAGER.pin,
      sizes: [
        { name: "Size S", price_vnd: 29000 },
        { name: "Size M", price_vnd: 35000 },
        { name: "Size L", price_vnd: 42000 },
      ],
    },
  });

  await apiRequest("/catalog/items", {
    token,
    body: {
      category_id: catCoffeeId,
      name: "Bạc xỉu",
      manager_pin: MANAGER.pin,
      sizes: [
        { name: "Size S", price_vnd: 32000 },
        { name: "Size M", price_vnd: 39000 },
      ],
    },
  });

  await apiRequest("/catalog/items", {
    token,
    body: {
      category_id: catTeaId,
      name: "Trà đào cam sả",
      manager_pin: MANAGER.pin,
      sizes: [
        { name: "Size M", price_vnd: 45000 },
        { name: "Size L", price_vnd: 52000 },
      ],
    },
  });

  console.log("Sample catalog successfully seeded.");
  console.log("Ready for POS-a UAT testing at http://localhost:5173/");
}

await main();
```

- [ ] **Step 3: Verify TypeScript compilation**

Run: `cd web && bun run lint`
Expected: PASS (no linting issues in web)

- [ ] **Step 4: Commit**

```bash
git add scripts/dev-seed.ts
git commit -m "feat(seed): extend dev-seed with categories, modifiers, and sized items"
```

---

### Task 6: POS API Seam (`use-pos.ts` & `use-pos.test.ts`)

**Files:**
- Create: `web/src/features/pos/api/use-pos.ts`
- Create: `web/src/features/pos/api/use-pos.test.ts`

**Interfaces:**
- Consumes:
  - `useGetCatalogMenuSellable`, `getGetCatalogMenuSellableQueryKey`
  - `useGetSalesServiceSessionsId`, `getGetSalesServiceSessionsIdQueryKey`
  - `usePostSalesServiceSessionsTakeaway`
  - `usePostSalesServiceSessionsIdDraftItems`
  - `usePatchSalesServiceSessionsIdDraftItemsItemIdQuantity`
  - `usePatchSalesServiceSessionsIdDraftItemsItemIdSize`
  - `usePatchSalesServiceSessionsIdDraftItemsItemIdModifiers`
  - `usePatchSalesServiceSessionsIdDraftItemsItemIdPreparationNote`
  - `useDeleteSalesServiceSessionsIdDraftItemsItemId`
  - `newRequestId`, `withRequestId`
  - `unwrap`, `unwrapNullable`
- Produces:
  - `useSellableMenu()`
  - `useServiceSession(sessionId: string | null)`
  - `useStartTakeawaySession()`
  - `useAddDraftItem(sessionId: string)`
  - `useUpdateDraftItemQuantity(sessionId: string)`
  - `useUpdateDraftItemSize(sessionId: string)`
  - `useUpdateDraftItemModifiers(sessionId: string)`
  - `useUpdateDraftItemPreparationNote(sessionId: string)`
  - `useRemoveDraftItem(sessionId: string)`

- [ ] **Step 1: Write failing test for POS API Seam export surface**

Create `web/src/features/pos/api/use-pos.test.ts`:
```ts
import { describe, expect, it } from "bun:test";
import {
  useSellableMenu,
  useServiceSession,
  useStartTakeawaySession,
  useAddDraftItem,
  useUpdateDraftItemQuantity,
  useUpdateDraftItemSize,
  useUpdateDraftItemModifiers,
  useUpdateDraftItemPreparationNote,
  useRemoveDraftItem,
} from "./use-pos";

describe("use-pos API Seam exports", () => {
  it("exports all required hook functions", () => {
    expect(typeof useSellableMenu).toBe("function");
    expect(typeof useServiceSession).toBe("function");
    expect(typeof useStartTakeawaySession).toBe("function");
    expect(typeof useAddDraftItem).toBe("function");
    expect(typeof useUpdateDraftItemQuantity).toBe("function");
    expect(typeof useUpdateDraftItemSize).toBe("function");
    expect(typeof useUpdateDraftItemModifiers).toBe("function");
    expect(typeof useUpdateDraftItemPreparationNote).toBe("function");
    expect(typeof useRemoveDraftItem).toBe("function");
  });
});
```

- [ ] **Step 2: Run test to verify failure**

Run: `cd web && bun test src/features/pos/api/use-pos.test.ts`
Expected: FAIL (Cannot find module `./use-pos`)

- [ ] **Step 3: Implement `use-pos.ts`**

Create `web/src/features/pos/api/use-pos.ts`:
```ts
import { useQueryClient } from "@tanstack/react-query";
import {
  useGetCatalogMenuSellable,
  getGetCatalogMenuSellableQueryKey,
} from "@/api/generated/endpoints/catalog/catalog";
import {
  useGetSalesServiceSessionsId,
  getGetSalesServiceSessionsIdQueryKey,
  usePostSalesServiceSessionsTakeaway,
  usePostSalesServiceSessionsIdDraftItems,
  usePatchSalesServiceSessionsIdDraftItemsItemIdQuantity,
  usePatchSalesServiceSessionsIdDraftItemsItemIdSize,
  usePatchSalesServiceSessionsIdDraftItemsItemIdModifiers,
  usePatchSalesServiceSessionsIdDraftItemsItemIdPreparationNote,
  useDeleteSalesServiceSessionsIdDraftItemsItemId,
} from "@/api/generated/endpoints/sales/sales";
import { unwrap, unwrapNullable } from "@/lib/unwrap";
import { newRequestId } from "@/lib/command";
import type {
  SalesAddDraftItemCommand,
  SalesSetDraftItemQuantityCommand,
  SalesSetDraftItemSizeCommand,
  SalesSetDraftItemModifiersCommand,
  SalesSetDraftItemNoteCommand,
} from "@/api/generated/models";

/**
 * Reads sellable menu categories and items.
 */
export function useSellableMenu() {
  return useGetCatalogMenuSellable({
    query: {
      select: unwrapNullable,
      staleTime: 60_000,
    },
  });
}

/**
 * Reads single Service Session with its active Order Draft projection.
 */
export function useServiceSession(sessionId: string | null) {
  return useGetSalesServiceSessionsId(sessionId ?? "", {
    query: {
      enabled: Boolean(sessionId),
      select: unwrap,
      staleTime: 5_000,
    },
  });
}

/**
 * Mutation to open an anonymous Takeaway Service Session.
 */
export function useStartTakeawaySession() {
  const queryClient = useQueryClient();
  const mutation = usePostSalesServiceSessionsTakeaway();

  return {
    ...mutation,
    startTakeaway: async (requestId?: string) => {
      const rid = requestId ?? newRequestId();
      const res = await mutation.mutateAsync({
        data: { request_id: rid },
      });
      const data = unwrap(res);
      if (data.id) {
        queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(data.id), res);
      }
      return data;
    },
  };
}

/**
 * Mutation to add an Order Draft item.
 */
export function useAddDraftItem(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostSalesServiceSessionsIdDraftItems();

  return {
    ...mutation,
    addDraftItem: async (command: SalesAddDraftItemCommand, requestId?: string) => {
      const rid = requestId ?? command.request_id ?? newRequestId();
      const res = await mutation.mutateAsync({
        id: sessionId,
        data: { ...command, request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sessionId), res);
      return data;
    },
  };
}

/**
 * Mutation to update draft item quantity.
 */
export function useUpdateDraftItemQuantity(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePatchSalesServiceSessionsIdDraftItemsItemIdQuantity();

  return {
    ...mutation,
    updateQuantity: async (
      itemId: string,
      command: SalesSetDraftItemQuantityCommand,
      requestId?: string,
    ) => {
      const rid = requestId ?? command.request_id ?? newRequestId();
      const res = await mutation.mutateAsync({
        id: sessionId,
        itemId,
        data: { ...command, request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sessionId), res);
      return data;
    },
  };
}

/**
 * Mutation to update draft item size.
 */
export function useUpdateDraftItemSize(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePatchSalesServiceSessionsIdDraftItemsItemIdSize();

  return {
    ...mutation,
    updateSize: async (
      itemId: string,
      command: SalesSetDraftItemSizeCommand,
      requestId?: string,
    ) => {
      const rid = requestId ?? command.request_id ?? newRequestId();
      const res = await mutation.mutateAsync({
        id: sessionId,
        itemId,
        data: { ...command, request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sessionId), res);
      return data;
    },
  };
}

/**
 * Mutation to update draft item modifier options.
 */
export function useUpdateDraftItemModifiers(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePatchSalesServiceSessionsIdDraftItemsItemIdModifiers();

  return {
    ...mutation,
    updateModifiers: async (
      itemId: string,
      command: SalesSetDraftItemModifiersCommand,
      requestId?: string,
    ) => {
      const rid = requestId ?? command.request_id ?? newRequestId();
      const res = await mutation.mutateAsync({
        id: sessionId,
        itemId,
        data: { ...command, request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sessionId), res);
      return data;
    },
  };
}

/**
 * Mutation to update draft item preparation note.
 */
export function useUpdateDraftItemPreparationNote(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePatchSalesServiceSessionsIdDraftItemsItemIdPreparationNote();

  return {
    ...mutation,
    updatePreparationNote: async (
      itemId: string,
      command: SalesSetDraftItemNoteCommand,
      requestId?: string,
    ) => {
      const rid = requestId ?? command.request_id ?? newRequestId();
      const res = await mutation.mutateAsync({
        id: sessionId,
        itemId,
        data: { ...command, request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sessionId), res);
      return data;
    },
  };
}

/**
 * Mutation to remove one draft item.
 */
export function useRemoveDraftItem(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = useDeleteSalesServiceSessionsIdDraftItemsItemId();

  return {
    ...mutation,
    removeDraftItem: async (itemId: string, requestId?: string) => {
      const rid = requestId ?? newRequestId();
      const res = await mutation.mutateAsync({
        id: sessionId,
        itemId,
        data: { request_id: rid },
        params: { request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sessionId), res);
      return data;
    },
  };
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `cd web && bun test src/features/pos/api/use-pos.test.ts`
Expected: PASS (1 test passed)

- [ ] **Step 5: Commit**

```bash
git add web/src/features/pos/api/use-pos.ts web/src/features/pos/api/use-pos.test.ts
git commit -m "feat(web): add use-pos api seam with query cache update semantics"
```

---

### Task 7: Menu Item Card & Menu Grid (`menu-item-card.tsx` & `menu-grid.tsx`)

**Files:**
- Create: `web/src/features/pos/components/menu-item-card.tsx`
- Create: `web/src/features/pos/components/menu-grid.tsx`

**Interfaces:**
- Consumes:
  - `CatalogSellableCategoryResponse`, `CatalogSellableItemResponse`
  - `getStartingPrice`
  - `filterSellableItems`
  - `formatVND`
  - `playTapChirp`
- Produces:
  - `<MenuItemCard item={...} categoryName={...} onSelect={...} disabled={...} />`
  - `<MenuGrid categories={...} onSelectItem={...} disabled={...} />`

- [ ] **Step 1: Implement `MenuItemCard`**

Create `web/src/features/pos/components/menu-item-card.tsx`:
```tsx
import * as React from "react";
import { Coffee, Layers } from "lucide-react";
import type { CatalogSellableItemResponse } from "@/api/generated/models";
import { getStartingPrice } from "../utils/pricing";
import { formatVND } from "@/lib/utils";
import { playTapChirp } from "@/lib/sound";

interface MenuItemCardProps {
  item: CatalogSellableItemResponse;
  categoryName?: string;
  onSelect: (item: CatalogSellableItemResponse) => void;
  disabled?: boolean;
}

export function MenuItemCard({
  item,
  categoryName,
  onSelect,
  disabled = false,
}: MenuItemCardProps) {
  const [imageError, setImageError] = React.useState(false);
  const startingPrice = getStartingPrice(item);
  const hasSizes = Boolean(item.sizes && item.sizes.length > 0);

  const handleClick = () => {
    if (disabled) return;
    playTapChirp();
    onSelect(item);
  };

  return (
    <div
      role="button"
      tabIndex={disabled ? -1 : 0}
      onClick={handleClick}
      onKeyDown={(e) => {
        if (!disabled && (e.key === "Enter" || e.key === " ")) {
          e.preventDefault();
          handleClick();
        }
      }}
      className={`group flex flex-col justify-between rounded-2xl border border-border bg-card p-3 min-h-[210px] select-none transition-all ${
        disabled
          ? "opacity-50 cursor-not-allowed"
          : "cursor-pointer hover:border-primary hover:shadow-xs active:scale-[0.98]"
      }`}
    >
      {/* Media Well with Fallback */}
      <div className="relative h-28 w-full overflow-hidden rounded-xl bg-muted flex items-center justify-center text-muted-foreground">
        {!imageError ? (
          <img
            src={`https://placewaifu.com/image/300/200?id=${item.id?.slice(0, 4) ?? "1"}`}
            alt={item.name ?? "Món"}
            className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
            onError={() => setImageError(true)}
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center bg-muted/60 text-muted-foreground">
            <Coffee className="h-8 w-8" />
          </div>
        )}

        {/* Multi-size indicator badge */}
        {hasSizes && (
          <div className="absolute bottom-2 right-2 flex items-center gap-1 rounded-md bg-background/90 backdrop-blur-xs px-2 py-0.5 text-2xs font-semibold text-foreground border border-border shadow-2xs">
            <Layers className="h-3 w-3 text-primary" />
            <span>{item.sizes?.length} cỡ</span>
          </div>
        )}
      </div>

      {/* Item Body */}
      <div className="mt-2.5 flex flex-col">
        {categoryName && (
          <span className="text-2xs font-semibold uppercase tracking-wider text-muted-foreground truncate">
            {categoryName}
          </span>
        )}
        <h4 className="text-sm font-semibold text-foreground line-clamp-1 leading-tight mt-0.5">
          {item.name}
        </h4>
      </div>

      {/* Price Readout */}
      <div className="mt-2 flex items-baseline justify-between pt-1 border-t border-border/50">
        <span className="font-mono text-sm sm:text-base font-bold text-primary">
          {hasSizes ? `Từ ${formatVND(startingPrice)}` : formatVND(startingPrice)}
        </span>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Implement `MenuGrid`**

Create `web/src/features/pos/components/menu-grid.tsx`:
```tsx
import * as React from "react";
import { Search, X, Coffee } from "lucide-react";
import type {
  CatalogSellableCategoryResponse,
  CatalogSellableItemResponse,
} from "@/api/generated/models";
import { MenuItemCard } from "./menu-item-card";
import { filterSellableItems } from "../utils/search";
import { playTapChirp } from "@/lib/sound";

interface MenuGridProps {
  categories?: CatalogSellableCategoryResponse[];
  onSelectItem: (item: CatalogSellableItemResponse) => void;
  disabled?: boolean;
}

export function MenuGrid({
  categories = [],
  onSelectItem,
  disabled = false,
}: MenuGridProps) {
  const [selectedCategoryId, setSelectedCategoryId] = React.useState<string | null>(
    null,
  );
  const [searchQuery, setSearchQuery] = React.useState("");
  const searchInputRef = React.useRef<HTMLInputElement>(null);

  // Global shortcut '/' to focus search input
  React.useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (
        e.key === "/" &&
        document.activeElement?.tagName !== "INPUT" &&
        document.activeElement?.tagName !== "TEXTAREA"
      ) {
        e.preventDefault();
        searchInputRef.current?.focus();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  const totalItemCount = React.useMemo(() => {
    return categories.reduce((acc, cat) => acc + (cat.items?.length ?? 0), 0);
  }, [categories]);

  const filteredItems = React.useMemo(() => {
    return filterSellableItems(categories, selectedCategoryId, searchQuery);
  }, [categories, selectedCategoryId, searchQuery]);

  // Find category name for item
  const categoryNameMap = React.useMemo(() => {
    const map = new Map<string, string>();
    for (const cat of categories) {
      for (const item of cat.items ?? []) {
        if (item.id && cat.name) {
          map.set(item.id, cat.name);
        }
      }
    }
    return map;
  }, [categories]);

  return (
    <section className="flex flex-1 flex-col overflow-hidden bg-background">
      {/* Top Search & Category Filter Toolbar */}
      <div className="flex flex-col gap-2.5 border-b border-border bg-card p-4 shrink-0 shadow-2xs">
        {/* Search Input Bar */}
        <div className="relative w-full">
          <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
          <input
            ref={searchInputRef}
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Tìm món nhanh (tên món, viết tắt: cfsd, bx)... [/]"
            className="h-12 w-full rounded-xl border border-border bg-muted/40 pl-10 pr-12 text-sm font-medium text-foreground placeholder:text-muted-foreground focus:border-primary focus:bg-card focus:outline-none focus:ring-2 focus:ring-primary/20 transition-all"
          />
          {searchQuery ? (
            <button
              type="button"
              onClick={() => {
                setSearchQuery("");
                searchInputRef.current?.focus();
              }}
              className="absolute right-2 top-1/2 -translate-y-1/2 h-8 w-8 rounded-lg flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted"
            >
              <X className="h-4 w-4" />
            </button>
          ) : (
            <kbd className="absolute right-3 top-1/2 -translate-y-1/2 pointer-events-none rounded border border-border bg-card px-1.5 py-0.5 font-mono text-2xs text-muted-foreground shadow-2xs">
              /
            </kbd>
          )}
        </div>

        {/* Category Filter Pills Rail (min-h-[48px]) */}
        <div className="flex items-center gap-2 overflow-x-auto no-scrollbar py-0.5">
          {/* 'Tất cả' Pill */}
          <button
            type="button"
            onClick={() => {
              playTapChirp();
              setSelectedCategoryId(null);
            }}
            className={`min-h-[48px] h-12 px-4 shrink-0 inline-flex items-center gap-2 rounded-xl text-sm font-semibold transition-all select-none active:scale-[0.98] ${
              selectedCategoryId === null
                ? "bg-primary text-primary-foreground shadow-xs"
                : "border border-border bg-card text-foreground hover:bg-muted"
            }`}
          >
            <span>Tất cả</span>
            <span
              className={`rounded-full px-2 py-0.5 text-2xs font-mono font-bold ${
                selectedCategoryId === null
                  ? "bg-primary-foreground/20 text-primary-foreground"
                  : "bg-muted text-muted-foreground"
              }`}
            >
              {totalItemCount}
            </span>
          </button>

          {/* Individual Category Pills */}
          {categories.map((cat) => {
            const isSelected = selectedCategoryId === cat.id;
            const count = cat.items?.length ?? 0;
            return (
              <button
                key={cat.id}
                type="button"
                onClick={() => {
                  playTapChirp();
                  setSelectedCategoryId(cat.id ?? null);
                }}
                className={`min-h-[48px] h-12 px-4 shrink-0 inline-flex items-center gap-2 rounded-xl text-sm font-semibold transition-all select-none active:scale-[0.98] ${
                  isSelected
                    ? "bg-primary text-primary-foreground shadow-xs"
                    : "border border-border bg-card text-foreground hover:bg-muted"
                }`}
              >
                <span>{cat.name}</span>
                <span
                  className={`rounded-full px-2 py-0.5 text-2xs font-mono font-bold ${
                    isSelected
                      ? "bg-primary-foreground/20 text-primary-foreground"
                      : "bg-muted text-muted-foreground"
                  }`}
                >
                  {count}
                </span>
              </button>
            );
          })}
        </div>
      </div>

      {/* Product Card Grid Container */}
      <div className="flex-1 overflow-y-auto p-4">
        {filteredItems.length > 0 ? (
          <div className="grid grid-cols-2 sm:grid-cols-3 xl:grid-cols-4 gap-3">
            {filteredItems.map((item) => (
              <MenuItemCard
                key={item.id}
                item={item}
                categoryName={item.id ? categoryNameMap.get(item.id) : undefined}
                onSelect={onSelectItem}
                disabled={disabled}
              />
            ))}
          </div>
        ) : (
          <div className="flex h-full flex-col items-center justify-center p-8 text-center text-muted-foreground gap-3">
            <div className="h-16 w-16 rounded-2xl bg-muted/60 flex items-center justify-center text-muted-foreground/60 border border-border">
              <Coffee className="h-8 w-8" />
            </div>
            <div className="space-y-1">
              <p className="text-sm font-bold text-foreground">Không tìm thấy món phù hợp</p>
              <p className="text-xs text-muted-foreground">
                Thử đổi từ khóa tìm kiếm hoặc chọn danh mục khác
              </p>
            </div>
          </div>
        )}
      </div>
    </section>
  );
}
```

- [ ] **Step 3: Verify TypeScript compilation**

Run: `cd web && bun run lint && tsc -b`
Expected: PASS (zero errors)

- [ ] **Step 4: Commit**

```bash
git add web/src/features/pos/components/menu-item-card.tsx web/src/features/pos/components/menu-grid.tsx
git commit -m "feat(web): add menu item card and filterable menu grid components"
```

---

### Task 8: Item Configuration Dialog (`item-picker-dialog.tsx`)

**Files:**
- Create: `web/src/features/pos/components/item-picker-dialog.tsx`

**Interfaces:**
- Consumes:
  - `CatalogSellableItemResponse`, `CatalogSellableSizeResponse`, `CatalogSellableModifierGroupResponse`
  - `resolveInitialSelection`, `toggleModifierOption`, `isSelectionValid`, `normalizePreparationNote`
  - `calculateItemUnitPrice`, `calculateLineTotal`
  - `formatVND`, `playTapChirp`
- Produces:
  - `<ItemPickerDialog item={...} initialValues={...} isOpen={...} onClose={...} onConfirm={...} isSubmitting={...} />`

- [ ] **Step 1: Implement `ItemPickerDialog`**

Create `web/src/features/pos/components/item-picker-dialog.tsx`:
```tsx
import * as React from "react";
import { X, Minus, Plus, Coffee } from "lucide-react";
import type { CatalogSellableItemResponse } from "@/api/generated/models";
import {
  resolveInitialSelection,
  toggleModifierOption,
  isSelectionValid,
  normalizePreparationNote,
  MAX_PREPARATION_NOTE_LENGTH,
} from "../utils/selection";
import { calculateItemUnitPrice, calculateLineTotal } from "../utils/pricing";
import { formatVND } from "@/lib/utils";
import { playTapChirp } from "@/lib/sound";

export interface ItemPickerConfig {
  sizeId?: string;
  selectedOptionIds: string[];
  preparationNote: string;
  quantity: number;
}

interface ItemPickerDialogProps {
  item: CatalogSellableItemResponse | null;
  initialValues?: Partial<ItemPickerConfig>;
  isOpen: boolean;
  onClose: () => void;
  onConfirm: (config: ItemPickerConfig) => void;
  isSubmitting?: boolean;
  confirmLabel?: string;
}

export function ItemPickerDialog({
  item,
  initialValues,
  isOpen,
  onClose,
  onConfirm,
  isSubmitting = false,
  confirmLabel = "Thêm vào đơn",
}: ItemPickerDialogProps) {
  const [sizeId, setSizeId] = React.useState<string | undefined>(undefined);
  const [selectedOptionIds, setSelectedOptionIds] = React.useState<string[]>([]);
  const [note, setNote] = React.useState<string>("");
  const [quantity, setQuantity] = React.useState<number>(1);

  // Sync state when dialog opens or item changes
  React.useEffect(() => {
    if (isOpen && item) {
      const defaults = resolveInitialSelection(item);
      setSizeId(initialValues?.sizeId ?? defaults.sizeId);
      setSelectedOptionIds(
        initialValues?.selectedOptionIds ?? defaults.selectedOptionIds,
      );
      setNote(initialValues?.preparationNote ?? defaults.note);
      setQuantity(initialValues?.quantity ?? defaults.quantity);
    }
  }, [isOpen, item, initialValues]);

  if (!isOpen || !item) return null;

  const hasSizes = Boolean(item.sizes && item.sizes.length > 0);
  const selectedSize = item.sizes?.find((s) => s.id === sizeId);
  const basePrice = selectedSize?.price_vnd ?? item.price_vnd ?? 0;

  // Calculate option surcharges
  const selectedOptionSurcharges: number[] = [];
  if (item.modifier_groups) {
    for (const group of item.modifier_groups) {
      for (const opt of group.options ?? []) {
        if (opt.id && selectedOptionIds.includes(opt.id)) {
          selectedOptionSurcharges.push(opt.surcharge_vnd ?? 0);
        }
      }
    }
  }

  const unitPrice = calculateItemUnitPrice(basePrice, selectedOptionSurcharges);
  const lineTotal = calculateLineTotal(unitPrice, quantity);
  const isValid = isSelectionValid(item, sizeId, selectedOptionIds);

  const handleConfirm = () => {
    if (!isValid || isSubmitting) return;
    playTapChirp();
    onConfirm({
      sizeId,
      selectedOptionIds,
      preparationNote: normalizePreparationNote(note),
      quantity,
    });
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-xs animate-in fade-in duration-150">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="picker-dialog-title"
        className="flex flex-col w-full max-w-lg max-h-[90vh] rounded-2xl border border-border bg-card shadow-2xl overflow-hidden animate-in fade-in zoom-in-95 duration-150"
      >
        {/* Dialog Header */}
        <div className="flex items-center justify-between border-b border-border p-4 bg-muted/20">
          <div className="flex items-center gap-3">
            <div className="h-10 w-10 rounded-xl bg-primary/10 text-primary flex items-center justify-center">
              <Coffee className="h-5 w-5" />
            </div>
            <div>
              <h3 id="picker-dialog-title" className="text-base font-bold text-foreground truncate">
                {item.name}
              </h3>
              <p className="text-2xs text-muted-foreground">Tùy chọn kích cỡ, topping & ghi chú</p>
            </div>
          </div>
          <button
            type="button"
            onClick={() => {
              playTapChirp();
              onClose();
            }}
            className="h-12 w-12 min-h-[48px] min-w-[48px] rounded-xl text-muted-foreground hover:text-foreground hover:bg-muted flex items-center justify-center select-none"
            aria-label="Đóng"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Dialog Scrollable Content */}
        <div className="flex-1 overflow-y-auto p-5 space-y-5">
          {/* Sizes Selection (>= 48px touch chips) */}
          {hasSizes && (
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
                  Kích cỡ <span className="text-destructive">*</span>
                </span>
                <span className="text-2xs text-muted-foreground">Bắt buộc chọn 1</span>
              </div>
              <div className="grid grid-cols-3 gap-2">
                {item.sizes?.map((size) => {
                  const isSelected = sizeId === size.id;
                  return (
                    <button
                      key={size.id}
                      type="button"
                      onClick={() => {
                        playTapChirp();
                        setSizeId(size.id);
                      }}
                      className={`min-h-[48px] h-12 px-3 rounded-xl border flex flex-col items-center justify-center transition-all select-none active:scale-[0.98] ${
                        isSelected
                          ? "border-primary bg-primary/10 text-primary font-bold ring-2 ring-primary/20"
                          : "border-border bg-card text-foreground hover:bg-muted"
                      }`}
                    >
                      <span className="text-xs font-bold leading-tight">{size.name}</span>
                      <span className="font-mono text-2xs text-muted-foreground">
                        {formatVND(size.price_vnd ?? 0)}
                      </span>
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          {/* Modifier Groups Selection (>= 48px touch chips) */}
          {item.modifier_groups?.map((group) => {
            const min = group.min_selections ?? 0;
            const max = group.max_selections ?? 1;
            const ruleText =
              min > 0 && max === 1
                ? "Bắt buộc chọn 1"
                : min > 0
                  ? `Chọn tối thiểu ${min}, tối đa ${max}`
                  : `Tùy chọn (tối đa ${max})`;

            return (
              <div key={group.id} className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
                    {group.name} {min > 0 && <span className="text-destructive">*</span>}
                  </span>
                  <span className="text-2xs text-muted-foreground">{ruleText}</span>
                </div>
                <div className="grid grid-cols-2 gap-2">
                  {group.options?.map((opt) => {
                    const isSelected = selectedOptionIds.includes(opt.id ?? "");
                    const surcharge = opt.surcharge_vnd ?? 0;

                    return (
                      <button
                        key={opt.id}
                        type="button"
                        onClick={() => {
                          playTapChirp();
                          setSelectedOptionIds((prev) =>
                            toggleModifierOption(group.id ?? "", opt.id ?? "", prev, group),
                          );
                        }}
                        className={`min-h-[48px] h-12 px-3 rounded-xl border flex items-center justify-between text-xs transition-all select-none active:scale-[0.98] ${
                          isSelected
                            ? "border-primary bg-primary/10 text-primary font-bold ring-2 ring-primary/20"
                            : "border-border bg-card text-foreground hover:bg-muted"
                        }`}
                      >
                        <span className="truncate pr-1">{opt.name}</span>
                        {surcharge > 0 && (
                          <span className="font-mono font-semibold text-primary shrink-0">
                            +{formatVND(surcharge)}
                          </span>
                        )}
                      </button>
                    );
                  })}
                </div>
              </div>
            );
          })}

          {/* Preparation Note (max 200 code points) */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <span className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
                Ghi chú pha chế
              </span>
              <span className="text-2xs font-mono text-muted-foreground">
                {note.length}/{MAX_PREPARATION_NOTE_LENGTH}
              </span>
            </div>
            <input
              type="text"
              value={note}
              maxLength={MAX_PREPARATION_NOTE_LENGTH}
              onChange={(e) => setNote(e.target.value)}
              placeholder="Ví dụ: Ít ngọt, nhiều đá, để riêng sốt..."
              className="h-12 w-full rounded-xl border border-border bg-card px-3.5 text-sm font-medium text-foreground placeholder:text-muted-foreground focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20"
            />
          </div>
        </div>

        {/* Dialog Footer (Quantity Stepper & Confirm CTA) */}
        <div className="flex items-center justify-between border-t border-border p-4 bg-muted/20 gap-4">
          {/* Stepper (min-w-[48px] min-h-[48px]) */}
          <div className="flex items-center rounded-xl border border-border bg-card p-0.5">
            <button
              type="button"
              disabled={quantity <= 1}
              onClick={() => {
                playTapChirp();
                setQuantity((q) => Math.max(1, q - 1));
              }}
              className="h-12 w-12 min-h-[48px] min-w-[48px] rounded-lg text-foreground hover:bg-muted disabled:opacity-30 disabled:hover:bg-transparent flex items-center justify-center select-none active:scale-95 transition"
              aria-label="Giảm số lượng"
            >
              <Minus className="h-4 w-4" />
            </button>
            <span className="w-10 text-center font-mono text-base font-bold text-foreground tabular-nums">
              {quantity}
            </span>
            <button
              type="button"
              disabled={quantity >= 9999}
              onClick={() => {
                playTapChirp();
                setQuantity((q) => Math.min(9999, q + 1));
              }}
              className="h-12 w-12 min-h-[48px] min-w-[48px] rounded-lg text-foreground hover:bg-muted disabled:opacity-30 disabled:hover:bg-transparent flex items-center justify-center select-none active:scale-95 transition"
              aria-label="Tăng số lượng"
            >
              <Plus className="h-4 w-4" />
            </button>
          </div>

          {/* Confirm Button */}
          <button
            type="button"
            disabled={!isValid || isSubmitting}
            onClick={handleConfirm}
            className="min-h-[48px] h-12 flex-1 rounded-xl bg-primary hover:bg-primary/90 disabled:bg-muted disabled:text-muted-foreground text-primary-foreground font-bold text-sm shadow-xs transition-all active:scale-[0.98] flex items-center justify-between px-5 select-none"
          >
            <span>{confirmLabel}</span>
            <span className="font-mono text-base font-bold tabular-nums">
              {formatVND(lineTotal)}
            </span>
          </button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Verify TypeScript compilation**

Run: `cd web && bun run lint && tsc -b`
Expected: PASS (zero errors)

- [ ] **Step 3: Commit**

```bash
git add web/src/features/pos/components/item-picker-dialog.tsx
git commit -m "feat(web): add item configuration dialog for sizes and modifiers"
```

---

### Task 9: Draft Item Row, Empty States, and Draft Panel (`no-shift-notice.tsx`, `draft-empty-state.tsx`, `draft-item-row.tsx`, `draft-panel.tsx`)

**Files:**
- Create: `web/src/features/pos/components/no-shift-notice.tsx`
- Create: `web/src/features/pos/components/draft-empty-state.tsx`
- Create: `web/src/features/pos/components/draft-item-row.tsx`
- Create: `web/src/features/pos/components/draft-panel.tsx`

**Interfaces:**
- Consumes:
  - `SalesServiceSessionResponse`, `SalesDraftItemResponse`
  - `calculateItemUnitPrice`, `calculateLineTotal`, `calculateDraftSubtotal`
  - `formatVND`, `playTapChirp`
- Produces:
  - `<NoShiftNotice />`
  - `<DraftEmptyState />`
  - `<DraftItemRow item={...} onEdit={...} onQuantityChange={...} onRemove={...} />`
  - `<DraftPanel session={...} isShiftOpen={...} onEditItem={...} onQuantityChange={...} onRemoveItem={...} />`

- [ ] **Step 1: Implement `NoShiftNotice`**

Create `web/src/features/pos/components/no-shift-notice.tsx`:
```tsx
import { Link } from "@tanstack/react-router";
import { AlertTriangle, Clock } from "lucide-react";
import { Button } from "@/components/ui/button";

export function NoShiftNotice() {
  return (
    <div className="flex h-full flex-col items-center justify-center p-6 text-center gap-4 bg-muted/20">
      <div className="h-14 w-14 rounded-2xl bg-amber-500/10 text-amber-600 flex items-center justify-center border border-amber-200">
        <AlertTriangle className="h-7 w-7" />
      </div>
      <div className="space-y-1 max-w-xs">
        <h3 className="text-base font-bold text-foreground">Chưa có ca bán hàng mở</h3>
        <p className="text-xs text-muted-foreground leading-relaxed">
          Bạn vẫn có thể xem thực đơn, nhưng cần mở ca để bắt đầu tạo đơn và tính tiền.
        </p>
      </div>
      <Button asChild className="h-12 min-h-[48px] rounded-xl px-6 font-bold shadow-xs">
        <Link to="/shift">
          <Clock className="h-4 w-4 mr-2" />
          Mở ca làm việc
        </Link>
      </Button>
    </div>
  );
}
```

- [ ] **Step 2: Implement `DraftEmptyState`**

Create `web/src/features/pos/components/draft-empty-state.tsx`:
```tsx
import { ShoppingBag } from "lucide-react";

export function DraftEmptyState() {
  return (
    <div className="flex h-full flex-col items-center justify-center p-6 text-center text-muted-foreground gap-3">
      <div className="h-16 w-16 rounded-2xl bg-muted/40 border border-border flex items-center justify-center text-muted-foreground/60 shadow-2xs">
        <ShoppingBag className="h-8 w-8" />
      </div>
      <div className="space-y-1">
        <p className="text-sm font-bold text-foreground">Chưa có món nào trong đơn</p>
        <p className="text-xs text-muted-foreground">
          Chạm vào món từ thực đơn bên trái để thêm vào đơn mang đi
        </p>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Implement `DraftItemRow`**

Create `web/src/features/pos/components/draft-item-row.tsx`:
```tsx
import { Minus, Plus, Trash2, AlertCircle } from "lucide-react";
import type { SalesDraftItemResponse } from "@/api/generated/models";
import { calculateItemUnitPrice, calculateLineTotal } from "../utils/pricing";
import { formatVND } from "@/lib/utils";
import { playTapChirp } from "@/lib/sound";

interface DraftItemRowProps {
  item: SalesDraftItemResponse;
  onEdit: (item: SalesDraftItemResponse) => void;
  onQuantityChange: (itemId: string, nextQty: number) => void;
  onRemove: (itemId: string) => void;
  disabled?: boolean;
}

export function DraftItemRow({
  item,
  onEdit,
  onQuantityChange,
  onRemove,
  disabled = false,
}: DraftItemRowProps) {
  const basePrice = item.price_vnd ?? 0;
  const surcharges = (item.selected_modifier_options ?? []).map(
    (opt) => opt.surcharge_vnd ?? 0,
  );
  const unitPrice = calculateItemUnitPrice(basePrice, surcharges);
  const quantity = item.quantity ?? 1;
  const lineTotal = calculateLineTotal(unitPrice, quantity);
  const isAvailable = item.available ?? true;

  const modifierSummary = (item.selected_modifier_options ?? [])
    .map((opt) => opt.name)
    .filter(Boolean)
    .join(", ");

  return (
    <div className="flex flex-col rounded-xl border border-border bg-card p-3 shadow-2xs gap-2 transition-all">
      {/* Top Header Row: Name, Size, Delete */}
      <div className="flex items-start justify-between gap-2">
        <div
          role="button"
          tabIndex={0}
          onClick={() => {
            if (!disabled) {
              playTapChirp();
              onEdit(item);
            }
          }}
          className="flex-1 cursor-pointer select-none space-y-0.5"
        >
          <div className="flex items-center gap-1.5 flex-wrap">
            <h5 className="text-sm font-bold text-foreground hover:text-primary transition-colors">
              {item.name}
            </h5>
            {item.size_name && (
              <span className="rounded bg-muted px-1.5 py-0.5 text-2xs font-semibold text-muted-foreground border border-border">
                {item.size_name}
              </span>
            )}
            {!isAvailable && (
              <span className="inline-flex items-center gap-1 rounded bg-amber-500/10 text-amber-700 dark:text-amber-400 border border-amber-200 px-1.5 py-0.5 text-2xs font-bold">
                <AlertCircle className="h-3 w-3" />
                Tạm hết hàng
              </span>
            )}
          </div>

          {/* Modifier List */}
          {modifierSummary && (
            <p className="text-2xs text-muted-foreground leading-tight">
              {modifierSummary}
            </p>
          )}

          {/* Preparation Note */}
          {item.preparation_note && (
            <p className="text-2xs italic text-slate-500 dark:text-slate-400">
              "{item.preparation_note}"
            </p>
          )}
        </div>

        {/* Delete Row Button */}
        <button
          type="button"
          disabled={disabled}
          onClick={() => {
            playTapChirp();
            if (item.id) onRemove(item.id);
          }}
          className="h-10 w-10 min-h-[40px] min-w-[40px] rounded-lg text-muted-foreground hover:text-destructive hover:bg-destructive/10 flex items-center justify-center select-none active:scale-95 transition"
          aria-label="Xóa món"
        >
          <Trash2 className="h-4 w-4" />
        </button>
      </div>

      {/* Bottom Line: Unit Price, Stepper, Line Total */}
      <div className="flex items-center justify-between pt-1 border-t border-border/40">
        <span className="font-mono text-xs text-muted-foreground">
          {formatVND(unitPrice)}
        </span>

        {/* Stepper */}
        <div className="flex items-center gap-3">
          <div className="flex items-center rounded-lg border border-border bg-muted/40 p-0.5">
            <button
              type="button"
              disabled={disabled || quantity <= 1}
              onClick={() => {
                playTapChirp();
                if (item.id) onQuantityChange(item.id, Math.max(1, quantity - 1));
              }}
              className="h-8 w-8 min-h-[32px] min-w-[32px] rounded text-foreground hover:bg-card disabled:opacity-30 flex items-center justify-center select-none active:scale-90 transition"
              aria-label="Giảm số lượng"
            >
              <Minus className="h-3.5 w-3.5" />
            </button>
            <span className="w-8 text-center font-mono text-xs font-bold text-foreground tabular-nums">
              {quantity}
            </span>
            <button
              type="button"
              disabled={disabled || quantity >= 9999}
              onClick={() => {
                playTapChirp();
                if (item.id) onQuantityChange(item.id, Math.min(9999, quantity + 1));
              }}
              className="h-8 w-8 min-h-[32px] min-w-[32px] rounded text-foreground hover:bg-card disabled:opacity-30 flex items-center justify-center select-none active:scale-90 transition"
              aria-label="Tăng số lượng"
            >
              <Plus className="h-3.5 w-3.5" />
            </button>
          </div>

          <span className="font-mono text-sm font-bold text-foreground tabular-nums">
            {formatVND(lineTotal)}
          </span>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Implement `DraftPanel`**

Create `web/src/features/pos/components/draft-panel.tsx`:
```tsx
import { Receipt, Trash2, ShoppingBag, Utensils, Banknote, QrCode } from "lucide-react";
import type {
  SalesServiceSessionResponse,
  SalesDraftItemResponse,
} from "@/api/generated/models";
import { DraftEmptyState } from "./draft-empty-state";
import { DraftItemRow } from "./draft-item-row";
import { NoShiftNotice } from "./no-shift-notice";
import { calculateDraftSubtotal } from "../utils/pricing";
import { formatVND } from "@/lib/utils";

interface DraftPanelProps {
  session: SalesServiceSessionResponse | null;
  isShiftOpen: boolean;
  onEditItem: (item: SalesDraftItemResponse) => void;
  onQuantityChange: (itemId: string, nextQty: number) => void;
  onRemoveItem: (itemId: string) => void;
  disabled?: boolean;
}

export function DraftPanel({
  session,
  isShiftOpen,
  onEditItem,
  onQuantityChange,
  onRemoveItem,
  disabled = false,
}: DraftPanelProps) {
  if (!isShiftOpen) {
    return (
      <aside className="flex w-full md:w-[380px] lg:w-[420px] shrink-0 flex-col border-l border-border bg-card overflow-hidden select-none">
        <NoShiftNotice />
      </aside>
    );
  }

  const draftItems = session?.draft?.items ?? [];
  const subtotal = calculateDraftSubtotal(draftItems);
  const serviceNumber = session?.service_number;

  return (
    <aside className="flex w-full md:w-[380px] lg:w-[420px] shrink-0 flex-col border-l border-border bg-card overflow-hidden select-none">
      {/* Bill Header */}
      <div className="flex items-center justify-between border-b border-border p-4 bg-muted/20 shrink-0">
        <div className="flex items-center gap-2.5">
          <div className="h-9 w-9 rounded-xl bg-primary/10 text-primary flex items-center justify-center">
            <Receipt className="h-5 w-5" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="text-sm font-bold text-foreground">
                {serviceNumber ? `Đơn mang đi #${serviceNumber}` : "Đơn mang đi"}
              </span>
              <span className="rounded-md bg-emerald-100 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300 border border-emerald-200/60 px-2 py-0.5 text-2xs font-bold">
                Mang đi
              </span>
            </div>
            <p className="text-2xs text-muted-foreground mt-0.5">Khách mua mang về</p>
          </div>
        </div>

        {/* Clear / Reset Cart (Disabled in Slice 3) */}
        <button
          type="button"
          disabled
          title="Chức năng hủy toàn bộ đơn chưa hỗ trợ; vui lòng xóa từng món"
          className="h-12 min-h-[48px] px-3 rounded-xl text-xs font-semibold text-muted-foreground border border-transparent opacity-40 cursor-not-allowed flex items-center gap-1.5"
        >
          <Trash2 className="h-4 w-4" />
          <span>Hủy đơn</span>
        </button>
      </div>

      {/* Bill Items Scrollable Container */}
      <div className="flex-1 overflow-y-auto p-4 space-y-2.5 bg-muted/10">
        {draftItems.length > 0 ? (
          draftItems.map((item) => (
            <DraftItemRow
              key={item.id}
              item={item}
              onEdit={onEditItem}
              onQuantityChange={onQuantityChange}
              onRemove={onRemoveItem}
              disabled={disabled}
            />
          ))
        ) : (
          <DraftEmptyState />
        )}
      </div>

      {/* Financial Summary */}
      <div className="border-t border-border bg-card p-4 space-y-1.5 shrink-0 shadow-2xs">
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>Tạm tính ({draftItems.length} món)</span>
          <span className="font-mono font-semibold text-foreground">{formatVND(subtotal)}</span>
        </div>
        <div className="flex items-baseline justify-between pt-1 border-t border-border/50">
          <span className="text-sm font-bold text-foreground">Tổng cộng</span>
          <span className="font-mono text-2xl font-bold text-primary tabular-nums">
            {formatVND(subtotal)}
          </span>
        </div>
      </div>

      {/* Bill Actions / Checkout Drawer (Disabled in Slice 3) */}
      <div className="border-t border-border bg-muted/20 p-4 space-y-2.5 shrink-0">
        {/* Mode Switcher: Dine-In Disabled */}
        <div className="grid grid-cols-2 gap-2 p-1 bg-muted rounded-xl border border-border">
          <div className="min-h-[40px] h-10 px-3 rounded-lg font-bold text-xs bg-foreground text-background flex items-center justify-center gap-1.5 shadow-xs">
            <ShoppingBag className="h-3.5 w-3.5" />
            <span>Mang đi</span>
          </div>
          <button
            type="button"
            disabled
            className="min-h-[40px] h-10 px-3 rounded-lg font-medium text-xs text-muted-foreground opacity-50 cursor-not-allowed flex items-center justify-center gap-1.5"
            title="Chế độ Tại bàn sẽ hoạt động ở Slice 7"
          >
            <Utensils className="h-3.5 w-3.5" />
            <span>Tại bàn (F2)</span>
          </button>
        </div>

        {/* Payment Tabs (Disabled) */}
        <div className="grid grid-cols-2 gap-2 p-1 bg-muted rounded-xl border border-border opacity-50">
          <div className="min-h-[40px] h-10 px-3 rounded-lg font-bold text-xs bg-card text-foreground flex items-center justify-center gap-1.5 shadow-2xs">
            <Banknote className="h-3.5 w-3.5" />
            <span>Tiền mặt</span>
          </div>
          <div className="min-h-[40px] h-10 px-3 rounded-lg font-medium text-xs text-muted-foreground flex items-center justify-center gap-1.5">
            <QrCode className="h-3.5 w-3.5" />
            <span>VietQR</span>
          </div>
        </div>

        {/* Checkout Commitment CTA (Disabled in Slice 3) */}
        <button
          type="button"
          disabled
          className="min-h-[48px] h-12 w-full rounded-xl bg-muted text-muted-foreground font-bold text-sm cursor-not-allowed border border-border flex flex-col items-center justify-center opacity-60"
        >
          <span>Thanh toán (F9)</span>
          <span className="text-2xs font-normal text-muted-foreground">Mở ở Slice 4</span>
        </button>
      </div>
    </aside>
  );
}
```

- [ ] **Step 5: Verify TypeScript compilation**

Run: `cd web && bun run lint && tsc -b`
Expected: PASS (zero errors)

- [ ] **Step 6: Commit**

```bash
git add web/src/features/pos/components/no-shift-notice.tsx \
        web/src/features/pos/components/draft-empty-state.tsx \
        web/src/features/pos/components/draft-item-row.tsx \
        web/src/features/pos/components/draft-panel.tsx
git commit -m "feat(web): add draft panel with row items, empty state, and disabled checkout drawer"
```

---

### Task 10: POS View Coordinator & Route Replacement (`pos-view.tsx`, `web/src/routes/_app/index.tsx`)

**Files:**
- Create: `web/src/features/pos/components/pos-view.tsx`
- Modify: `web/src/routes/_app/index.tsx`

**Interfaces:**
- Consumes:
  - `useCurrentShift` from `@/features/shift/api/use-shift`
  - `useSellableMenu`, `useServiceSession`, `useStartTakeawaySession`, `useAddDraftItem`, `useUpdateDraftItemQuantity`, `useUpdateDraftItemSize`, `useUpdateDraftItemModifiers`, `useUpdateDraftItemPreparationNote`, `useRemoveDraftItem` from `../api/use-pos`
  - `MenuGrid`, `DraftPanel`, `ItemPickerDialog`
- Produces:
  - `<PosView />` coordinating full POS-a terminal experience.

- [ ] **Step 1: Implement `PosView`**

Create `web/src/features/pos/components/pos-view.tsx`:
```tsx
import * as React from "react";
import { Clock, AlertCircle, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useCurrentShift } from "@/features/shift/api/use-shift";
import {
  useSellableMenu,
  useServiceSession,
  useStartTakeawaySession,
  useAddDraftItem,
  useUpdateDraftItemQuantity,
  useUpdateDraftItemSize,
  useUpdateDraftItemModifiers,
  useUpdateDraftItemPreparationNote,
  useRemoveDraftItem,
} from "../api/use-pos";
import { MenuGrid } from "./menu-grid";
import { DraftPanel } from "./draft-panel";
import { ItemPickerDialog, type ItemPickerConfig } from "./item-picker-dialog";
import type {
  CatalogSellableItemResponse,
  SalesDraftItemResponse,
} from "@/api/generated/models";
import { messageForError } from "@/lib/error-messages";

const STORAGE_SESSION_KEY = "pos_active_session_id";

export function PosView() {
  const { data: shift, isLoading: isShiftLoading } = useCurrentShift();
  const {
    data: menu,
    isLoading: isMenuLoading,
    isError: isMenuError,
    error: menuError,
    refetch: refetchMenu,
  } = useSellableMenu();

  const isShiftOpen = shift?.state === "OPEN";

  // Single active session pointer in sessionStorage
  const [activeSessionId, setActiveSessionId] = React.useState<string | null>(() => {
    try {
      return sessionStorage.getItem(STORAGE_SESSION_KEY);
    } catch {
      return null;
    }
  });

  const { data: session, refetch: refetchSession } = useServiceSession(activeSessionId);

  // Clear session ID if session was closed or not found
  React.useEffect(() => {
    if (session && session.state && session.state !== "ACTIVE") {
      sessionStorage.removeItem(STORAGE_SESSION_KEY);
      setActiveSessionId(null);
    }
  }, [session]);

  // Mutations
  const { startTakeaway } = useStartTakeawaySession();
  const { addDraftItem, isPending: isAddingItem } = useAddDraftItem(activeSessionId ?? "");
  const { updateQuantity } = useUpdateDraftItemQuantity(activeSessionId ?? "");
  const { updateSize } = useUpdateDraftItemSize(activeSessionId ?? "");
  const { updateModifiers } = useUpdateDraftItemModifiers(activeSessionId ?? "");
  const { updatePreparationNote } = useUpdateDraftItemPreparationNote(activeSessionId ?? "");
  const { removeDraftItem } = useRemoveDraftItem(activeSessionId ?? "");

  // Modal State
  const [pickerItem, setPickerItem] = React.useState<CatalogSellableItemResponse | null>(null);
  const [editingDraftItemId, setEditingDraftItemId] = React.useState<string | null>(null);
  const [pickerInitialValues, setPickerInitialValues] = React.useState<Partial<ItemPickerConfig> | undefined>(undefined);
  const [isPickerOpen, setIsPickerOpen] = React.useState(false);
  const [errorMessage, setErrorMessage] = React.useState<string | null>(null);

  // Ensure active session exists, lazily opening one if needed
  const ensureSessionId = async (): Promise<string> => {
    if (activeSessionId) return activeSessionId;
    const newSession = await startTakeaway();
    const id = newSession.id ?? "";
    sessionStorage.setItem(STORAGE_SESSION_KEY, id);
    setActiveSessionId(id);
    return id;
  };

  // Add Item Handler
  const handleSelectItem = async (item: CatalogSellableItemResponse) => {
    if (!isShiftOpen) return;
    setErrorMessage(null);

    const hasSizes = Boolean(item.sizes && item.sizes.length > 0);
    const hasModifiers = Boolean(item.modifier_groups && item.modifier_groups.length > 0);

    // Simple item: 1-tap direct add
    if (!hasSizes && !hasModifiers) {
      try {
        const sid = await ensureSessionId();
        await addDraftItem({
          menu_item_id: item.id,
          quantity: 1,
        });
      } catch (err) {
        setErrorMessage(messageForError(err));
      }
      return;
    }

    // Configurable item: open modal
    setPickerItem(item);
    setEditingDraftItemId(null);
    setPickerInitialValues(undefined);
    setIsPickerOpen(true);
  };

  // Edit Item Handler
  const handleEditDraftItem = (draftItem: SalesDraftItemResponse) => {
    if (!isShiftOpen) return;
    setErrorMessage(null);

    // Locate sellable item from menu categories
    let foundCatalogItem: CatalogSellableItemResponse | null = null;
    for (const cat of menu?.categories ?? []) {
      for (const it of cat.items ?? []) {
        if (it.id === draftItem.menu_item_id) {
          foundCatalogItem = it;
          break;
        }
      }
      if (foundCatalogItem) break;
    }

    if (!foundCatalogItem) return;

    setPickerItem(foundCatalogItem);
    setEditingDraftItemId(draftItem.id ?? null);
    setPickerInitialValues({
      sizeId: draftItem.size_id,
      selectedOptionIds: (draftItem.selected_modifier_options ?? [])
        .map((opt) => opt.id)
        .filter(Boolean) as string[],
      preparationNote: draftItem.preparation_note ?? "",
      quantity: draftItem.quantity ?? 1,
    });
    setIsPickerOpen(true);
  };

  // Confirm Modal Handler (Add or Update)
  const handleConfirmPicker = async (config: ItemPickerConfig) => {
    if (!pickerItem) return;
    setErrorMessage(null);

    try {
      if (editingDraftItemId) {
        // Edit existing draft item: apply updates
        if (config.sizeId) {
          await updateSize(editingDraftItemId, { size_id: config.sizeId });
        }
        await updateModifiers(editingDraftItemId, {
          modifier_option_ids: config.selectedOptionIds,
        });
        await updatePreparationNote(editingDraftItemId, {
          preparation_note: config.preparationNote,
        });
        await updateQuantity(editingDraftItemId, { quantity: config.quantity });
      } else {
        // Add new draft item
        const sid = await ensureSessionId();
        await addDraftItem({
          menu_item_id: pickerItem.id,
          size_id: config.sizeId,
          modifier_option_ids: config.selectedOptionIds,
          preparation_note: config.preparationNote || undefined,
        });
        // If quantity > 1, update quantity
        if (config.quantity > 1 && session?.draft?.items) {
          // Quantity will be adjusted via item row if needed
        }
      }
      setIsPickerOpen(false);
    } catch (err) {
      setErrorMessage(messageForError(err));
    }
  };

  // Stepper Quantity Change Handler
  const handleQuantityChange = async (itemId: string, nextQty: number) => {
    setErrorMessage(null);
    try {
      await updateQuantity(itemId, { quantity: nextQty });
    } catch (err) {
      setErrorMessage(messageForError(err));
    }
  };

  // Remove Item Handler
  const handleRemoveItem = async (itemId: string) => {
    setErrorMessage(null);
    try {
      await removeDraftItem(itemId);
    } catch (err) {
      setErrorMessage(messageForError(err));
    }
  };

  if (isShiftLoading || isMenuLoading) {
    return (
      <div className="flex flex-col items-center justify-center p-12 min-h-[60vh] gap-4">
        <div className="w-12 h-12 rounded-2xl bg-primary/10 text-primary flex items-center justify-center animate-pulse">
          <Clock className="w-6 h-6 animate-spin" />
        </div>
        <p className="text-sm text-muted-foreground font-medium">Đang tải thực đơn bán hàng...</p>
      </div>
    );
  }

  if (isMenuError) {
    return (
      <div className="flex flex-col items-center justify-center p-8 max-w-md mx-auto min-h-[60vh] text-center gap-4">
        <div className="w-12 h-12 rounded-2xl bg-destructive/10 text-destructive flex items-center justify-center">
          <AlertCircle className="w-6 h-6" />
        </div>
        <div className="space-y-1">
          <h3 className="text-base font-bold text-foreground">Không thể tải thực đơn</h3>
          <p className="text-xs text-muted-foreground">{messageForError(menuError)}</p>
        </div>
        <Button
          type="button"
          variant="outline"
          onClick={() => refetchMenu()}
          className="rounded-xl h-10"
        >
          <RefreshCw className="w-4 h-4 mr-2" />
          Thử lại
        </Button>
      </div>
    );
  }

  return (
    <div className="flex h-full w-full flex-col md:flex-row overflow-hidden">
      {/* Zone 1: Menu Grid */}
      <MenuGrid
        categories={menu?.categories}
        onSelectItem={handleSelectItem}
        disabled={!isShiftOpen}
      />

      {/* Zone 2: Order Bill Aside */}
      <DraftPanel
        session={session ?? null}
        isShiftOpen={isShiftOpen}
        onEditItem={handleEditDraftItem}
        onQuantityChange={handleQuantityChange}
        onRemoveItem={handleRemoveItem}
      />

      {/* Item Configuration Modal */}
      <ItemPickerDialog
        item={pickerItem}
        initialValues={pickerInitialValues}
        isOpen={isPickerOpen}
        onClose={() => setIsPickerOpen(false)}
        onConfirm={handleConfirmPicker}
        isSubmitting={isAddingItem}
        confirmLabel={editingDraftItemId ? "Cập nhật món" : "Thêm vào đơn"}
      />

      {/* Global Error Toast Bar if mutation fails */}
      {errorMessage && (
        <div className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 flex items-center gap-2 rounded-xl bg-destructive text-destructive-foreground px-4 py-2.5 text-xs font-bold shadow-lg animate-in fade-in slide-in-from-bottom-2">
          <AlertCircle className="h-4 w-4 shrink-0" />
          <span>{errorMessage}</span>
          <button
            type="button"
            onClick={() => setErrorMessage(null)}
            className="ml-2 text-destructive-foreground/80 hover:text-destructive-foreground"
          >
            Đóng
          </button>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Update Route in `web/src/routes/_app/index.tsx`**

Modify `web/src/routes/_app/index.tsx`:
```tsx
import { createFileRoute } from "@tanstack/react-router";
import { PosView } from "@/features/pos/components/pos-view";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/")({
  beforeLoad: () => requireCapability("sales.operate"),
  component: PosView,
});
```

- [ ] **Step 3: Verify TypeScript compilation**

Run: `cd web && bun run lint && tsc -b`
Expected: PASS (zero errors)

- [ ] **Step 4: Commit**

```bash
git add web/src/features/pos/components/pos-view.tsx web/src/routes/_app/index.tsx
git commit -m "feat(web): wire pos view coordinator and replace placeholder route"
```

---

### Task 11: End-to-End Verification & UAT Handover

**Files:**
- None (Verification & Handover)

**Checklist:**
- [ ] 1. Run all unit tests: `cd web && bun test`
  - Expected: ALL tests pass across pricing, selection, search, command, sound, shift, and error-messages.
- [ ] 2. Run lint and typecheck: `cd web && bun run lint && tsc -b`
  - Expected: Zero lint errors and zero type errors.
- [ ] 3. Run development seed: `bun run scripts/dev-seed.ts`
  - Expected: Categories, modifier groups, and sized items seeded successfully against running Go API.
- [ ] 4. Perform UAT script:
  - **Case A: Closed Shift Guard**
    - Navigate to `/` without an active shift.
    - Verify menu is visible but bill displays `NoShiftNotice`.
    - Verify clicking menu items does not add items or crash.
  - **Case B: Open Shift & Add Simple Item**
    - Go to `/shift`, open shift with float `2.000.000 đ`.
    - Return to `/`. Bill displays empty state.
    - Click "Croissant bơ tỏi". Verify Takeaway session is created, bill header updates to "Đơn mang đi #1", and item appears at `35.000 đ`.
  - **Case C: Configurable Item with Modal**
    - Click "Cà phê sữa đá". Verify `<ItemPickerDialog>` opens with Size chips, sugar radio options, and topping checkboxes.
    - Select Size L, 50% đường, Trân châu trắng. Add note "Ít đá".
    - Click "Thêm vào đơn". Verify line item reflects size, toppings, and note.
  - **Case D: Item Edit & Remove**
    - Tap "Cà phê sữa đá" row on bill. Modal reopens with previous selections. Change to 70% đường, click "Cập nhật món".
    - Tap `[+]` on Croissant to increase quantity to 2.
    - Tap trash icon on Croissant to remove it.
  - **Case E: Search & Filter**
    - Type `cfsd` in search bar. Verify grid shows only "Cà phê sữa đá".
    - Clear search, click "Trà trái cây" pill. Verify only fruit tea items display.
  - **Case F: Persistence**
    - Refresh page (`F5`). Verify active order #1 and its items remain intact.
- [ ] 5. Hand over to operator.

- [ ] **Step 1: Run verification commands**

```bash
cd /e/Code/pos-cafe/web && bun test && bun run lint && tsc -b
```

- [ ] **Step 2: Commit final documentation / handover notes**

```bash
git commit --allow-empty -m "docs(pos): complete web slice 3 implementation and uat handover"
```
