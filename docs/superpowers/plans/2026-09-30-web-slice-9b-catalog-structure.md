# Web Slice 9b — Catalog Structure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give a Manager the "Quản lý Thực đơn & Topping" tab under `/settings`: create, edit, and retire menu items (single price or Sizes), categories, and modifier groups, and assign groups in bulk through the Batch Linker with a live POS preview, all on the BA-1 commands.

**Architecture:** No Go code changes. A new `web/src/features/catalog/` folder reads `GET /catalog/menu/manage` and `GET /catalog/modifier-groups` into a flat, non-retired `CatalogModel`. Each form's "Lưu" runs a pure planner that diffs the form against the snapshot taken when the modal opened and emits an ordered list of commands (ADR-062). A runner executes them one by one, asks for the Manager PIN once when any command needs it, and stops at the first failure. Pressing "Lưu" again re-plans against the refreshed snapshot. The Batch Linker previews the result by rendering the POS's own `ItemPickerBody`, split out of `ItemPickerDialog`.

**Tech Stack:** React 19, TanStack Router + Query, orval-generated client (`@/api/generated/endpoints/catalog/catalog`), Tailwind, lucide-react, react-hotkeys-hook, zustand (`useManagerApprovalStore`), `bun test` with `react-dom/server` `renderToString`.

**Spec:** [`docs/superpowers/specs/2026-09-30-web-slice-9b-catalog-structure-design.md`](../specs/2026-09-30-web-slice-9b-catalog-structure-design.md)

## Global Constraints

- No Go, SQL, or Swagger change. Nothing is regenerated with `bun run codegen`.
- No new npm dependency.
- User-facing copy is Vietnamese and taken verbatim from this plan's code blocks. The key strings are "Quản lý Thực đơn & Topping", "Món & Định giá", "Danh mục món", "Nhóm Topping", "Ma trận Gán Topping (Batch Linker)", "LƯU MÓN ĂN (Ctrl+S)", "LƯU DANH MỤC", "LƯU NHÓM TOPPING", "LƯU ÁP DỤNG NGAY (Ctrl+S)", "Xóa món ăn", "Xóa danh mục", "Xóa nhóm topping", "Hủy bỏ", "Không thể hoàn tác. Lịch sử bán hàng vẫn được giữ.", and "Lưu chưa hoàn tất. Sửa lỗi rồi bấm Lưu để tiếp tục phần còn lại."
- Badge labels: BEST_SELLER "Bán chạy", HOT "Món hot", NEW "Mới", SIGNATURE "Đặc trưng", CHEF_PICK "Chef Pick". Retirement reasons: NO_LONGER_OFFERED "Không bán nữa", MENU_RESTRUCTURE "Sắp xếp lại thực đơn", OTHER "Khác" (requires a note).
- Every command gets a fresh `request_id` from `newRequestId()` (`@/lib/command`). A retry never replays; it re-plans against refreshed server state (ADR-062).
- These commands need `manager_pin`: `item.create`, `item.reprice`, `size.reprice`, `size.add`, `group.create`, `option.reprice`, `option.add`. No other command does.
- Limits: price 1 to 2,147,483,647 VND; surcharge 0 to 2,147,483,647; code `^[a-z0-9]{1,12}$` after trim and lowercase; description ≤ 300 characters; display order 0 to 9999; image long edge ≤ 800 px, encoded WebP, ≤ 1 MiB (1,048,576 bytes).
- Touch targets are at least 48 px (`min-h-[48px]`), per `design-system/pos-cafe/DESIGN.md`.
- POS behavior must not change: every existing test under `web/src/features/pos/` passes unchanged after Task 8.
- Web tests: pure logic and `renderToString` component checks only. No end-to-end tests, no browser integration tests (slice sequence §3).
- Web commands run from `web/`. `bunx vite build` deletes the tracked `web/dist/.gitkeep`; restore it after every build with `git checkout -- dist/.gitkeep`.
- Chrome reserves Ctrl+N outside kiosk or app mode. The shortcut is wired as the prototype asks, but the "+ THÊM MÓN MỚI" buttons are the path UAT relies on.

---

## File map

| File | Responsibility |
| --- | --- |
| `web/src/features/catalog/lib/catalog-model.ts` | `CatalogModel` types, `toCatalogModel`, badge and reason labels, `displayCode`, `priceLabel`, `ruleLabel`, `filterItems`, `currentAssignment`, `sameSet` |
| `web/src/features/catalog/lib/forms.ts` | Form state types, builders from a snapshot, row helpers |
| `web/src/features/catalog/lib/validation.ts` | Client-side validation of the three forms |
| `web/src/features/catalog/lib/item-inheritance.ts` | Inherited and effective groups; category change prunes exclusions |
| `web/src/features/catalog/lib/save-plan.ts` | `Command` union, planners, `commandNeedsPin`, `adoptRows` |
| `web/src/features/catalog/lib/run-plan.ts` | Sequential runner with NEW-id resolution and stop-on-failure |
| `web/src/features/catalog/lib/image-resize.ts` | Canvas resize to WebP under the size cap |
| `web/src/features/catalog/lib/sellable-preview.ts` | Builds a `CatalogSellableItemResponse` for the linker preview |
| `web/src/features/catalog/lib/linker.ts` | Batch Linker draft transitions |
| `web/src/features/catalog/lib/category-icons.ts` | The fixed Lucide icon set offered for categories |
| `web/src/features/catalog/lib/views.ts` | The four view keys and search-param parsing |
| `web/src/features/catalog/api/execute-command.ts` | Maps one `Command` to its generated endpoint |
| `web/src/features/catalog/api/use-catalog-admin.ts` | Queries, invalidation, `useCatalogSave`, `useRetireEntity` |
| `web/src/features/catalog/components/form-bits.tsx` | `ModalFrame`, `Field`, `Chip`, `FormFooter`, input classes |
| `web/src/features/catalog/components/save-progress.tsx` | Per-step result list after a failed save |
| `web/src/features/catalog/components/retire-dialog.tsx` | Reason-and-note confirmation for retirement |
| `web/src/features/catalog/components/item-form-panel.tsx`, `item-form-modal.tsx` | Item modal: presentation and save logic |
| `web/src/features/catalog/components/item-card.tsx`, `items-view.tsx` | "Món & Định giá" view |
| `web/src/features/catalog/components/category-form-panel.tsx`, `category-form-modal.tsx`, `categories-view.tsx` | "Danh mục món" view and modal |
| `web/src/features/catalog/components/group-form-panel.tsx`, `group-form-modal.tsx`, `groups-view.tsx` | "Nhóm Topping" view and modal |
| `web/src/features/catalog/components/linker-view.tsx` | "Ma trận Gán Topping" view |
| `web/src/features/catalog/components/catalog-nav.tsx`, `catalog-view.tsx` | Tab shell: header, pills, view switch, item modal host |
| `web/src/features/pos/components/item-picker-dialog.tsx` | Split into `ItemPickerBody` and the dialog shell |
| `web/src/features/settings/lib/tabs.ts` | Catalog tab moves from planned to routable |
| `web/src/routes/_app/settings/catalog.tsx` | Route, guard, `?view=` search param |
| `web/src/routes/_app/settings.tsx` | Counter rekeyed to `/settings/catalog` |
| `spec/decisions.md` | ADR-062 |

---

### Task 1: Catalog model

**Files:**
- Create: `web/src/features/catalog/lib/catalog-model.ts`
- Test: `web/src/features/catalog/lib/catalog-model.test.ts`

**Interfaces:**
- Consumes: generated types `CatalogManagementMenuResponse`, `CatalogManagementItemResponse`, `CatalogManagementModifierGroupResponse`; `getAcronym`, `normalizeVietnamese` from `@/lib/search`; `formatVND` from `@/lib/utils`.
- Produces: `Badge`, `BADGE_LABELS`, `BADGES`, `RetireReason`, `RETIRE_REASON_LABELS`, `RETIRE_REASONS`, `Retirement`, `CatSize`, `CatItem`, `CatCategory`, `CatOption`, `CatGroup`, `CatalogModel`, `Assignment`, `EMPTY_CATALOG`, `toCatalogModel(menu, groups)`, `displayCode(item)`, `priceLabel(item)`, `ruleLabel(group)`, `filterItems(model, query, categoryId)`, `currentAssignment(model, groupId)`, `sameSet(a, b)`.

- [ ] **Step 1: Write the failing test**

```ts
// web/src/features/catalog/lib/catalog-model.test.ts
import { describe, expect, it } from "bun:test";
import type {
  CatalogManagementMenuResponse,
  CatalogManagementModifierGroupResponse,
} from "@/api/generated/models";
import { formatVND } from "@/lib/utils";
import {
  currentAssignment,
  displayCode,
  filterItems,
  priceLabel,
  ruleLabel,
  sameSet,
  toCatalogModel,
} from "./catalog-model";

const groups: CatalogManagementModifierGroupResponse[] = [
  {
    id: "g-ice",
    name: "Mức đá",
    min_selections: 1,
    max_selections: 1,
    default_option_ids: ["o-full", "o-gone"],
    options: [
      { id: "o-full", name: "100% đá", surcharge_vnd: 0, available: true },
      { id: "o-gone", name: "Đá riêng", surcharge_vnd: 0, available: true, retired: true },
    ],
  },
  { id: "g-old", name: "Nhóm cũ", min_selections: 0, max_selections: 1, retired: true, options: [] },
];

const menu: CatalogManagementMenuResponse = {
  categories: [
    {
      id: "c-tea",
      name: "Trà",
      display_order: 2,
      modifier_group_ids: ["g-ice", "g-old"],
      items: [
        {
          id: "i-peach",
          category_id: "c-tea",
          name: "Trà đào cam sả",
          code: "tdcs",
          badge: "HOT",
          price_vnd: 45000,
          direct_modifier_group_ids: ["g-old"],
          excluded_modifier_group_ids: [],
        },
        { id: "i-dead", category_id: "c-tea", name: "Trà cũ", retired: true, price_vnd: 1 },
      ],
    },
    {
      id: "c-coffee",
      name: "Cà phê",
      display_order: 1,
      modifier_group_ids: [],
      items: [
        {
          id: "i-milk",
          category_id: "c-coffee",
          name: "Cà phê sữa đá",
          badge: "WHATEVER",
          sizes: [
            { id: "s-m", name: "M", price_vnd: 29000, available: true },
            { id: "s-l", name: "L", price_vnd: 35000, available: false },
            { id: "s-x", name: "XL", price_vnd: 1, retired: true },
          ],
          direct_modifier_group_ids: ["g-ice"],
          excluded_modifier_group_ids: [],
        },
      ],
    },
    { id: "c-gone", name: "Đã bỏ", retired: true, display_order: 0, items: [] },
  ],
};

describe("toCatalogModel", () => {
  const model = toCatalogModel(menu, groups);

  it("drops retired entities and orders categories by display order", () => {
    expect(model.categories.map((c) => c.id)).toEqual(["c-coffee", "c-tea"]);
    expect(model.items.map((i) => i.id)).toEqual(["i-milk", "i-peach"]);
    expect(model.items[0].sizes.map((s) => s.id)).toEqual(["s-m", "s-l"]);
    expect(model.groups.map((g) => g.id)).toEqual(["g-ice"]);
    expect(model.groups[0].options.map((o) => o.id)).toEqual(["o-full"]);
    expect(model.groups[0].defaultOptionIds).toEqual(["o-full"]);
  });

  it("drops references to retired groups", () => {
    expect(model.categories[1].groupIds).toEqual(["g-ice"]);
    expect(model.items[1].directGroupIds).toEqual([]);
  });

  it("keeps only known badges and marks sized items with a null price", () => {
    expect(model.items[1].badge).toBe("HOT");
    expect(model.items[0].badge).toBeNull();
    expect(model.items[0].priceVnd).toBeNull();
    expect(model.items[1].priceVnd).toBe(45000);
  });

  it("handles missing responses", () => {
    expect(toCatalogModel(undefined, undefined)).toEqual({ categories: [], items: [], groups: [] });
  });
});

describe("display helpers", () => {
  const model = toCatalogModel(menu, groups);

  it("falls back to the name acronym when no code is stored", () => {
    expect(displayCode(model.items[0])).toBe("cfsd");
    expect(displayCode(model.items[1])).toBe("tdcs");
  });

  it("labels a single price, a range, and an empty size list", () => {
    expect(priceLabel(model.items[1])).toBe(formatVND(45000));
    expect(priceLabel(model.items[0])).toBe(`${formatVND(29000)} – ${formatVND(35000)}`);
    expect(priceLabel({ priceVnd: null, sizes: [] })).toBe("—");
  });

  it("describes selection rules", () => {
    expect(ruleLabel({ min: 1, max: 1 })).toBe("Chọn 1 · bắt buộc");
    expect(ruleLabel({ min: 0, max: 1 })).toBe("Chọn 1");
    expect(ruleLabel({ min: 1, max: 3 })).toBe("Chọn 1–3");
    expect(ruleLabel({ min: 0, max: 5 })).toBe("Tối đa 5");
  });

  it("filters by category, name without diacritics, and code", () => {
    expect(filterItems(model, "", "all").map((i) => i.id)).toEqual(["i-milk", "i-peach"]);
    expect(filterItems(model, "", "c-tea").map((i) => i.id)).toEqual(["i-peach"]);
    expect(filterItems(model, "sua da", "all").map((i) => i.id)).toEqual(["i-milk"]);
    expect(filterItems(model, "TDC", "all").map((i) => i.id)).toEqual(["i-peach"]);
  });

  it("reports a group's direct attachments", () => {
    expect(currentAssignment(model, "g-ice")).toEqual({ itemIds: ["i-milk"], categoryIds: ["c-tea"] });
  });

  it("compares id lists as sets", () => {
    expect(sameSet(["a", "b"], ["b", "a"])).toBe(true);
    expect(sameSet(["a"], ["a", "b"])).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/catalog/lib/catalog-model.test.ts`
Expected: FAIL, `Cannot find module './catalog-model'`.

- [ ] **Step 3: Write the implementation**

```ts
// web/src/features/catalog/lib/catalog-model.ts
import type {
  CatalogManagementItemResponse,
  CatalogManagementMenuResponse,
  CatalogManagementModifierGroupResponse,
} from "@/api/generated/models";
import { getAcronym, normalizeVietnamese } from "@/lib/search";
import { formatVND } from "@/lib/utils";

export type Badge = "BEST_SELLER" | "HOT" | "NEW" | "SIGNATURE" | "CHEF_PICK";

/** Labels from settings.html's badge selector. */
export const BADGE_LABELS: Record<Badge, string> = {
  BEST_SELLER: "Bán chạy",
  HOT: "Món hot",
  NEW: "Mới",
  SIGNATURE: "Đặc trưng",
  CHEF_PICK: "Chef Pick",
};

export const BADGES = Object.keys(BADGE_LABELS) as Badge[];

export type RetireReason = "NO_LONGER_OFFERED" | "MENU_RESTRUCTURE" | "OTHER";

export const RETIRE_REASON_LABELS: Record<RetireReason, string> = {
  NO_LONGER_OFFERED: "Không bán nữa",
  MENU_RESTRUCTURE: "Sắp xếp lại thực đơn",
  OTHER: "Khác",
};

export const RETIRE_REASONS = Object.keys(RETIRE_REASON_LABELS) as RetireReason[];

export interface Retirement {
  reason: RetireReason;
  note: string;
}

export interface CatSize {
  id: string;
  name: string;
  priceVnd: number;
  available: boolean;
}

export interface CatItem {
  id: string;
  categoryId: string;
  name: string;
  code: string | null;
  badge: Badge | null;
  description: string | null;
  imageUrl: string | null;
  /** Set for a single-price item; null when the item is priced by Sizes (ADR-060). */
  priceVnd: number | null;
  sizes: CatSize[];
  directGroupIds: string[];
  excludedGroupIds: string[];
}

export interface CatCategory {
  id: string;
  name: string;
  icon: string | null;
  displayOrder: number;
  groupIds: string[];
}

export interface CatOption {
  id: string;
  name: string;
  surchargeVnd: number;
  available: boolean;
}

export interface CatGroup {
  id: string;
  name: string;
  min: number;
  max: number;
  options: CatOption[];
  defaultOptionIds: string[];
}

export interface CatalogModel {
  categories: CatCategory[];
  items: CatItem[];
  groups: CatGroup[];
}

/** A modifier group's direct attachments: what the Batch Linker edits. */
export interface Assignment {
  itemIds: string[];
  categoryIds: string[];
}

export const EMPTY_CATALOG: CatalogModel = { categories: [], items: [], groups: [] };

function toBadge(value: string | undefined): Badge | null {
  return value !== undefined && value in BADGE_LABELS ? (value as Badge) : null;
}

function toGroup(g: CatalogManagementModifierGroupResponse): CatGroup {
  const options = (g.options ?? [])
    .filter((o) => !o.retired)
    .map((o) => ({
      id: o.id ?? "",
      name: o.name ?? "",
      surchargeVnd: o.surcharge_vnd ?? 0,
      available: o.available ?? false,
    }));
  const live = new Set(options.map((o) => o.id));
  return {
    id: g.id ?? "",
    name: g.name ?? "",
    min: g.min_selections ?? 0,
    max: g.max_selections ?? 1,
    options,
    defaultOptionIds: (g.default_option_ids ?? []).filter((id) => live.has(id)),
  };
}

function toItem(i: CatalogManagementItemResponse, keep: (ids?: string[]) => string[]): CatItem {
  return {
    id: i.id ?? "",
    categoryId: i.category_id ?? "",
    name: i.name ?? "",
    code: i.code ?? null,
    badge: toBadge(i.badge),
    description: i.description ?? null,
    imageUrl: i.image_url ?? null,
    priceVnd: i.price_vnd ?? null,
    sizes: (i.sizes ?? [])
      .filter((s) => !s.retired)
      .map((s) => ({ id: s.id ?? "", name: s.name ?? "", priceVnd: s.price_vnd ?? 0, available: s.available ?? false })),
    directGroupIds: keep(i.direct_modifier_group_ids),
    excludedGroupIds: keep(i.excluded_modifier_group_ids),
  };
}

/**
 * Flattens the management projection into the non-retired catalog the admin
 * screens edit. Retired entities have no reinstate command, so they are
 * dropped, and so are references to retired groups.
 */
export function toCatalogModel(
  menu: CatalogManagementMenuResponse | undefined,
  groups: CatalogManagementModifierGroupResponse[] | undefined,
): CatalogModel {
  const liveGroups = (groups ?? []).filter((g) => !g.retired).map(toGroup);
  const liveIds = new Set(liveGroups.map((g) => g.id));
  const keep = (ids?: string[]) => (ids ?? []).filter((id) => liveIds.has(id));

  const live = (menu?.categories ?? [])
    .filter((c) => !c.retired)
    .map((raw) => ({
      raw,
      category: {
        id: raw.id ?? "",
        name: raw.name ?? "",
        icon: raw.icon ?? null,
        displayOrder: raw.display_order ?? 0,
        groupIds: keep(raw.modifier_group_ids),
      },
    }));
  live.sort(
    (a, b) =>
      a.category.displayOrder - b.category.displayOrder || a.category.name.localeCompare(b.category.name, "vi"),
  );

  return {
    categories: live.map((l) => l.category),
    items: live.flatMap((l) => (l.raw.items ?? []).filter((i) => !i.retired).map((i) => toItem(i, keep))),
    groups: liveGroups,
  };
}

export function displayCode(item: Pick<CatItem, "code" | "name">): string {
  return item.code ?? getAcronym(item.name);
}

export function priceLabel(item: Pick<CatItem, "priceVnd" | "sizes">): string {
  if (item.priceVnd !== null) return formatVND(item.priceVnd);
  const prices = item.sizes.map((s) => s.priceVnd);
  if (prices.length === 0) return "—";
  const lo = Math.min(...prices);
  const hi = Math.max(...prices);
  return lo === hi ? formatVND(lo) : `${formatVND(lo)} – ${formatVND(hi)}`;
}

export function ruleLabel(group: Pick<CatGroup, "min" | "max">): string {
  if (group.max === 1) return group.min >= 1 ? "Chọn 1 · bắt buộc" : "Chọn 1";
  return group.min > 0 ? `Chọn ${group.min}–${group.max}` : `Tối đa ${group.max}`;
}

/** Name matches ignore diacritics; code matches are prefix matches on the shown code. */
export function filterItems(model: CatalogModel, query: string, categoryId: string): CatItem[] {
  const q = normalizeVietnamese(query);
  const compact = q.replace(/\s/g, "");
  return model.items.filter((item) => {
    if (categoryId !== "all" && item.categoryId !== categoryId) return false;
    if (!q) return true;
    return normalizeVietnamese(item.name).includes(q) || displayCode(item).toLowerCase().startsWith(compact);
  });
}

export function currentAssignment(model: CatalogModel, groupId: string): Assignment {
  return {
    itemIds: model.items.filter((i) => i.directGroupIds.includes(groupId)).map((i) => i.id),
    categoryIds: model.categories.filter((c) => c.groupIds.includes(groupId)).map((c) => c.id),
  };
}

/** Id lists never hold duplicates, so equal length plus containment is set equality. */
export function sameSet(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((id) => b.includes(id));
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && bun test src/features/catalog/lib/catalog-model.test.ts`
Expected: PASS, 10 tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/catalog/lib/catalog-model.ts web/src/features/catalog/lib/catalog-model.test.ts
git commit -m "feat(web): catalog admin model from the management projection"
```

---

### Task 2: Form state and validation

**Files:**
- Create: `web/src/features/catalog/lib/forms.ts`
- Create: `web/src/features/catalog/lib/validation.ts`
- Test: `web/src/features/catalog/lib/forms.test.ts`
- Test: `web/src/features/catalog/lib/validation.test.ts`

**Interfaces:**
- Consumes: `Badge`, `CatItem`, `CatCategory`, `CatGroup`, `Retirement` (Task 1); `newRequestId` from `@/lib/command`.
- Produces:
  - `forms.ts`: `SizeRow`, `OptionRow`, `PendingRetire`, `ImageChange`, `ItemForm`, `CategoryForm`, `GroupForm`, `SelectionType`, `blankSizeRow()`, `blankOptionRow()`, `itemFormFrom(item, defaultCategoryId)`, `categoryFormFrom(category, categories)`, `groupFormFrom(group)`, `removeSizeRow(form, key, retirement)`, `removeOptionRow(form, key, retirement)`, `toggleDefault(form, key)`, `selectionType(form)`, `setSelectionType(form, type)`.
  - `validation.ts`: `FieldErrors`, `MAX_VND`, `MAX_DESCRIPTION`, `nameKey(name)`, `hasErrors(errors)`, `validateItemForm(form)`, `validateCategoryForm(form)`, `validateGroupForm(form)`.

- [ ] **Step 1: Write the failing tests**

```ts
// web/src/features/catalog/lib/forms.test.ts
import { describe, expect, it } from "bun:test";
import type { CatCategory, CatGroup, CatItem } from "./catalog-model";
import {
  categoryFormFrom,
  groupFormFrom,
  itemFormFrom,
  removeOptionRow,
  removeSizeRow,
  selectionType,
  setSelectionType,
  toggleDefault,
} from "./forms";

const latte: CatItem = {
  id: "i-latte",
  categoryId: "c-coffee",
  name: "Latte",
  code: "lt",
  badge: "HOT",
  description: null,
  imageUrl: null,
  priceVnd: null,
  sizes: [
    { id: "s-m", name: "M", priceVnd: 39000, available: true },
    { id: "s-l", name: "L", priceVnd: 45000, available: true },
  ],
  directGroupIds: ["g-top"],
  excludedGroupIds: [],
};

const topping: CatGroup = {
  id: "g-top",
  name: "Topping",
  min: 0,
  max: 2,
  options: [
    { id: "o-pearl", name: "Trân châu", surchargeVnd: 10000, available: true },
    { id: "o-jelly", name: "Thạch", surchargeVnd: 8000, available: true },
  ],
  defaultOptionIds: ["o-jelly"],
};

const retire = { reason: "NO_LONGER_OFFERED" as const, note: "" };

describe("item form", () => {
  it("starts a new item as single price in the given category", () => {
    const form = itemFormFrom(null, "c-tea");
    expect(form.mode).toBe("single");
    expect(form.categoryId).toBe("c-tea");
    expect(form.sizes).toEqual([]);
  });

  it("loads a sized item with rows keyed by size id", () => {
    const form = itemFormFrom(latte, "c-tea");
    expect(form.mode).toBe("sizes");
    expect(form.sizes.map((s) => [s.key, s.id, s.name])).toEqual([
      ["s-m", "s-m", "M"],
      ["s-l", "s-l", "L"],
    ]);
    expect(form.code).toBe("lt");
    expect(form.directGroupIds).toEqual(["g-top"]);
  });

  it("queues a saved size for retirement and drops an unsaved one", () => {
    const form = itemFormFrom(latte, "c-tea");
    const withNew = { ...form, sizes: [...form.sizes, { key: "k-new", id: null, name: "XL", priceVnd: 1 }] };
    const retired = removeSizeRow(withNew, "s-l", retire);
    expect(retired.sizes.map((s) => s.key)).toEqual(["s-m", "k-new"]);
    expect(retired.retiredSizes).toEqual([{ id: "s-l", name: "L", retirement: retire }]);
    const dropped = removeSizeRow(withNew, "k-new", null);
    expect(dropped.retiredSizes).toEqual([]);
    expect(dropped.sizes.map((s) => s.key)).toEqual(["s-m", "s-l"]);
  });
});

describe("category form", () => {
  it("suggests the next display order for a new category", () => {
    const cats: CatCategory[] = [
      { id: "a", name: "A", icon: null, displayOrder: 3, groupIds: [] },
      { id: "b", name: "B", icon: null, displayOrder: 7, groupIds: [] },
    ];
    expect(categoryFormFrom(null, cats).displayOrder).toBe(8);
    expect(categoryFormFrom(cats[0], cats)).toEqual({ name: "A", icon: null, displayOrder: 3, groupIds: [] });
  });
});

describe("group form", () => {
  it("marks stored defaults on their rows", () => {
    const form = groupFormFrom(topping);
    expect(form.rows.map((r) => [r.id, r.isDefault])).toEqual([
      ["o-pearl", false],
      ["o-jelly", true],
    ]);
  });

  it("starts a new group with one blank row", () => {
    const form = groupFormFrom(null);
    expect(form.rows).toHaveLength(1);
    expect(form.min).toBe(0);
    expect(form.max).toBe(1);
  });

  it("keeps at most one default in a single-choice group", () => {
    const single = setSelectionType(groupFormFrom(topping), "single");
    expect(single.max).toBe(1);
    const toggled = toggleDefault(single, "o-pearl");
    expect(toggled.rows.map((r) => r.isDefault)).toEqual([true, false]);
  });

  it("lets a multiple-choice group hold several defaults", () => {
    const toggled = toggleDefault(groupFormFrom(topping), "o-pearl");
    expect(toggled.rows.map((r) => r.isDefault)).toEqual([true, true]);
  });

  it("derives the selection type from max and widens it for multiple", () => {
    expect(selectionType({ max: 1 })).toBe("single");
    expect(selectionType({ max: 3 })).toBe("multiple");
    expect(setSelectionType(groupFormFrom(null), "multiple").max).toBe(2);
  });

  it("queues a saved option for retirement", () => {
    const form = removeOptionRow(groupFormFrom(topping), "o-pearl", retire);
    expect(form.rows.map((r) => r.id)).toEqual(["o-jelly"]);
    expect(form.retiredOptions).toEqual([{ id: "o-pearl", name: "Trân châu", retirement: retire }]);
  });
});
```

```ts
// web/src/features/catalog/lib/validation.test.ts
import { describe, expect, it } from "bun:test";
import { groupFormFrom, itemFormFrom, type GroupForm, type ItemForm } from "./forms";
import { hasErrors, validateCategoryForm, validateGroupForm, validateItemForm } from "./validation";

const validItem: ItemForm = { ...itemFormFrom(null, "c-tea"), name: "Trà đào", priceVnd: 45000 };

function group(patch: Partial<GroupForm>): GroupForm {
  return {
    ...groupFormFrom(null),
    name: "Topping",
    rows: [
      { key: "a", id: null, name: "Trân châu", surchargeVnd: 10000, isDefault: false },
      { key: "b", id: null, name: "Thạch", surchargeVnd: 0, isDefault: false },
    ],
    min: 0,
    max: 2,
    ...patch,
  };
}

describe("validateItemForm", () => {
  it("accepts a valid single-price item", () => {
    expect(validateItemForm(validItem)).toEqual({});
  });

  it("requires a name, a category, and a positive price", () => {
    const errors = validateItemForm({ ...validItem, name: "  ", categoryId: "", priceVnd: 0 });
    expect(Object.keys(errors).sort()).toEqual(["categoryId", "name", "priceVnd"]);
  });

  it("requires at least one size and checks each row", () => {
    expect(validateItemForm({ ...validItem, mode: "sizes", sizes: [] }).sizes).toBe("Cần ít nhất một kích cỡ");
    const errors = validateItemForm({
      ...validItem,
      mode: "sizes",
      sizes: [
        { key: "a", id: null, name: "M", priceVnd: 30000 },
        { key: "b", id: null, name: " m ", priceVnd: 35000 },
        { key: "c", id: null, name: "L", priceVnd: 1.5 },
      ],
    });
    expect(errors["size.a"]).toBe("Tên kích cỡ bị trùng");
    expect(errors["size.b"]).toBe("Tên kích cỡ bị trùng");
    expect(errors["size.c"]).toBe("Giá bán phải từ 1 VND");
  });

  it("checks the code pattern after lowercasing", () => {
    expect(validateItemForm({ ...validItem, code: "CFSD" })).toEqual({});
    expect(validateItemForm({ ...validItem, code: "cà-phê" }).code).toBeDefined();
    expect(validateItemForm({ ...validItem, code: "a".repeat(13) }).code).toBeDefined();
  });

  it("limits the description to 300 characters", () => {
    expect(validateItemForm({ ...validItem, description: "đ".repeat(300) })).toEqual({});
    expect(validateItemForm({ ...validItem, description: "đ".repeat(301) }).description).toBe("Mô tả tối đa 300 ký tự");
  });
});

describe("validateCategoryForm", () => {
  it("requires a name and an order from 0 to 9999", () => {
    expect(validateCategoryForm({ name: "Trà", icon: null, displayOrder: 0, groupIds: [] })).toEqual({});
    const errors = validateCategoryForm({ name: "", icon: null, displayOrder: 10000, groupIds: [] });
    expect(Object.keys(errors).sort()).toEqual(["displayOrder", "name"]);
  });
});

describe("validateGroupForm", () => {
  it("accepts a valid group", () => {
    expect(validateGroupForm(group({}))).toEqual({});
  });

  it("requires rows with unique, non-empty names and non-negative surcharges", () => {
    expect(validateGroupForm(group({ rows: [] })).rows).toBe("Cần ít nhất một lựa chọn");
    const errors = validateGroupForm(
      group({
        rows: [
          { key: "a", id: null, name: "Thạch", surchargeVnd: 0, isDefault: false },
          { key: "b", id: null, name: "thạch", surchargeVnd: 0, isDefault: false },
          { key: "c", id: null, name: "", surchargeVnd: -1, isDefault: false },
        ],
        max: 3,
      }),
    );
    expect(errors["row.a"]).toBe("Tên lựa chọn bị trùng");
    expect(errors["row.c"]).toBe("Vui lòng nhập tên lựa chọn");
  });

  it("bounds max by the row count and min by max", () => {
    expect(validateGroupForm(group({ max: 3 })).max).toBe("Tối đa phải từ 1 đến số lựa chọn");
    expect(validateGroupForm(group({ max: 0 })).max).toBe("Tối đa phải từ 1 đến số lựa chọn");
    expect(validateGroupForm(group({ min: 3, max: 2 })).min).toBe("Tối thiểu phải từ 0 đến tối đa");
  });

  it("keeps the default count between min and max", () => {
    expect(validateGroupForm(group({ min: 1 })).defaults).toBe("Số lựa chọn mặc định phải từ tối thiểu đến tối đa");
    const both = group({ max: 1, rows: group({}).rows.map((r) => ({ ...r, isDefault: true })) });
    expect(validateGroupForm(both).defaults).toBeDefined();
  });

  it("reports whether any error exists", () => {
    expect(hasErrors({})).toBe(false);
    expect(hasErrors({ name: "x" })).toBe(true);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun test src/features/catalog/lib/forms.test.ts src/features/catalog/lib/validation.test.ts`
Expected: FAIL, `Cannot find module './forms'`.

- [ ] **Step 3: Write `forms.ts`**

```ts
// web/src/features/catalog/lib/forms.ts
import { newRequestId } from "@/lib/command";
import type { Badge, CatCategory, CatGroup, CatItem, Retirement } from "./catalog-model";

/** `id` is null until the row exists on the server; `key` is stable for React. */
export interface SizeRow {
  key: string;
  id: string | null;
  name: string;
  priceVnd: number;
}

export interface OptionRow {
  key: string;
  id: string | null;
  name: string;
  surchargeVnd: number;
  isDefault: boolean;
}

/** A saved Size or Option the form will retire when it is saved. */
export interface PendingRetire {
  id: string;
  name: string;
  retirement: Retirement;
}

export type ImageChange =
  | { kind: "keep" }
  | { kind: "upload"; blob: Blob; previewUrl: string }
  | { kind: "clear" };

export interface ItemForm {
  name: string;
  categoryId: string;
  mode: "single" | "sizes";
  priceVnd: number;
  sizes: SizeRow[];
  retiredSizes: PendingRetire[];
  code: string;
  badge: Badge | null;
  description: string;
  image: ImageChange;
  directGroupIds: string[];
  excludedGroupIds: string[];
}

export interface CategoryForm {
  name: string;
  icon: string | null;
  displayOrder: number;
  groupIds: string[];
}

export interface GroupForm {
  name: string;
  min: number;
  max: number;
  rows: OptionRow[];
  retiredOptions: PendingRetire[];
}

export type SelectionType = "single" | "multiple";

export function blankSizeRow(): SizeRow {
  return { key: newRequestId(), id: null, name: "", priceVnd: 0 };
}

export function blankOptionRow(): OptionRow {
  return { key: newRequestId(), id: null, name: "", surchargeVnd: 0, isDefault: false };
}

export function itemFormFrom(item: CatItem | null, defaultCategoryId: string): ItemForm {
  if (!item) {
    return {
      name: "",
      categoryId: defaultCategoryId,
      mode: "single",
      priceVnd: 0,
      sizes: [],
      retiredSizes: [],
      code: "",
      badge: null,
      description: "",
      image: { kind: "keep" },
      directGroupIds: [],
      excludedGroupIds: [],
    };
  }
  return {
    name: item.name,
    categoryId: item.categoryId,
    mode: item.priceVnd === null ? "sizes" : "single",
    priceVnd: item.priceVnd ?? 0,
    sizes: item.sizes.map((s) => ({ key: s.id, id: s.id, name: s.name, priceVnd: s.priceVnd })),
    retiredSizes: [],
    code: item.code ?? "",
    badge: item.badge,
    description: item.description ?? "",
    image: { kind: "keep" },
    directGroupIds: [...item.directGroupIds],
    excludedGroupIds: [...item.excludedGroupIds],
  };
}

export function categoryFormFrom(category: CatCategory | null, categories: readonly CatCategory[]): CategoryForm {
  if (category) {
    return { name: category.name, icon: category.icon, displayOrder: category.displayOrder, groupIds: [...category.groupIds] };
  }
  const next = categories.reduce((max, c) => Math.max(max, c.displayOrder), 0) + 1;
  return { name: "", icon: null, displayOrder: Math.min(next, 9999), groupIds: [] };
}

export function groupFormFrom(group: CatGroup | null): GroupForm {
  if (!group) return { name: "", min: 0, max: 1, rows: [blankOptionRow()], retiredOptions: [] };
  return {
    name: group.name,
    min: group.min,
    max: group.max,
    rows: group.options.map((o) => ({
      key: o.id,
      id: o.id,
      name: o.name,
      surchargeVnd: o.surchargeVnd,
      isDefault: group.defaultOptionIds.includes(o.id),
    })),
    retiredOptions: [],
  };
}

/** Removing a saved row queues its retirement; an unsaved row just disappears. */
export function removeSizeRow(form: ItemForm, key: string, retirement: Retirement | null): ItemForm {
  const row = form.sizes.find((s) => s.key === key);
  if (!row) return form;
  const retiredSizes =
    row.id && retirement ? [...form.retiredSizes, { id: row.id, name: row.name, retirement }] : form.retiredSizes;
  return { ...form, sizes: form.sizes.filter((s) => s.key !== key), retiredSizes };
}

export function removeOptionRow(form: GroupForm, key: string, retirement: Retirement | null): GroupForm {
  const row = form.rows.find((r) => r.key === key);
  if (!row) return form;
  const retiredOptions =
    row.id && retirement ? [...form.retiredOptions, { id: row.id, name: row.name, retirement }] : form.retiredOptions;
  return { ...form, rows: form.rows.filter((r) => r.key !== key), retiredOptions };
}

/** A single-choice group (max 1) keeps at most one default. */
export function toggleDefault(form: GroupForm, key: string): GroupForm {
  const single = form.max === 1;
  return {
    ...form,
    rows: form.rows.map((r) =>
      r.key === key ? { ...r, isDefault: !r.isDefault } : single ? { ...r, isDefault: false } : r,
    ),
  };
}

/** The backend stores only bounds; "single" is max 1. */
export function selectionType(form: Pick<GroupForm, "max">): SelectionType {
  return form.max === 1 ? "single" : "multiple";
}

export function setSelectionType(form: GroupForm, type: SelectionType): GroupForm {
  if (type === "multiple") return { ...form, max: Math.max(2, form.rows.length) };
  let kept = false;
  return {
    ...form,
    max: 1,
    min: Math.min(form.min, 1),
    rows: form.rows.map((r) => {
      if (!r.isDefault) return r;
      if (kept) return { ...r, isDefault: false };
      kept = true;
      return r;
    }),
  };
}
```

- [ ] **Step 4: Write `validation.ts`**

```ts
// web/src/features/catalog/lib/validation.ts
import type { CategoryForm, GroupForm, ItemForm } from "./forms";

export type FieldErrors = Record<string, string>;

export const MAX_VND = 2_147_483_647;
export const MAX_DESCRIPTION = 300;
const CODE_PATTERN = /^[a-z0-9]{1,12}$/;

/** The server compares names trimmed and lowercased. */
export function nameKey(name: string): string {
  return name.trim().toLocaleLowerCase("vi");
}

export function hasErrors(errors: FieldErrors): boolean {
  return Object.keys(errors).length > 0;
}

const isPrice = (v: number) => Number.isInteger(v) && v >= 1 && v <= MAX_VND;
const isSurcharge = (v: number) => Number.isInteger(v) && v >= 0 && v <= MAX_VND;

function duplicateKeys(rows: readonly { key: string; name: string }[]): Set<string> {
  const first = new Map<string, string>();
  const dup = new Set<string>();
  for (const row of rows) {
    const k = nameKey(row.name);
    if (!k) continue;
    const seen = first.get(k);
    if (seen) {
      dup.add(seen);
      dup.add(row.key);
    } else {
      first.set(k, row.key);
    }
  }
  return dup;
}

export function validateItemForm(form: ItemForm): FieldErrors {
  const errors: FieldErrors = {};
  if (!form.name.trim()) errors.name = "Vui lòng nhập tên món";
  if (!form.categoryId) errors.categoryId = "Vui lòng chọn danh mục";
  if (form.mode === "single") {
    if (!isPrice(form.priceVnd)) errors.priceVnd = "Giá bán phải từ 1 VND";
  } else {
    if (form.sizes.length === 0) errors.sizes = "Cần ít nhất một kích cỡ";
    const dup = duplicateKeys(form.sizes);
    for (const s of form.sizes) {
      const key = `size.${s.key}`;
      if (!s.name.trim()) errors[key] = "Vui lòng nhập tên kích cỡ";
      else if (dup.has(s.key)) errors[key] = "Tên kích cỡ bị trùng";
      else if (!isPrice(s.priceVnd)) errors[key] = "Giá bán phải từ 1 VND";
    }
  }
  const code = form.code.trim().toLowerCase();
  if (code && !CODE_PATTERN.test(code)) errors.code = "Mã chỉ gồm chữ cái không dấu và số, tối đa 12 ký tự";
  if (Array.from(form.description.trim()).length > MAX_DESCRIPTION) errors.description = "Mô tả tối đa 300 ký tự";
  return errors;
}

export function validateCategoryForm(form: CategoryForm): FieldErrors {
  const errors: FieldErrors = {};
  if (!form.name.trim()) errors.name = "Vui lòng nhập tên danh mục";
  if (!Number.isInteger(form.displayOrder) || form.displayOrder < 0 || form.displayOrder > 9999) {
    errors.displayOrder = "Thứ tự hiển thị từ 0 đến 9999";
  }
  return errors;
}

export function validateGroupForm(form: GroupForm): FieldErrors {
  const errors: FieldErrors = {};
  if (!form.name.trim()) errors.name = "Vui lòng nhập tên nhóm";
  if (form.rows.length === 0) errors.rows = "Cần ít nhất một lựa chọn";
  const dup = duplicateKeys(form.rows);
  for (const r of form.rows) {
    const key = `row.${r.key}`;
    if (!r.name.trim()) errors[key] = "Vui lòng nhập tên lựa chọn";
    else if (dup.has(r.key)) errors[key] = "Tên lựa chọn bị trùng";
    else if (!isSurcharge(r.surchargeVnd)) errors[key] = "Giá thêm phải từ 0 VND";
  }
  const { min, max } = form;
  if (!Number.isInteger(max) || max < 1 || max > form.rows.length) errors.max = "Tối đa phải từ 1 đến số lựa chọn";
  if (!Number.isInteger(min) || min < 0 || min > max) errors.min = "Tối thiểu phải từ 0 đến tối đa";
  const defaults = form.rows.filter((r) => r.isDefault).length;
  if (defaults < min || defaults > max) errors.defaults = "Số lựa chọn mặc định phải từ tối thiểu đến tối đa";
  return errors;
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd web && bun test src/features/catalog/lib/forms.test.ts src/features/catalog/lib/validation.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/catalog/lib/forms.ts web/src/features/catalog/lib/forms.test.ts web/src/features/catalog/lib/validation.ts web/src/features/catalog/lib/validation.test.ts
git commit -m "feat(web): catalog admin form state and validation"
```

---

### Task 3: Item inheritance

**Files:**
- Create: `web/src/features/catalog/lib/item-inheritance.ts`
- Test: `web/src/features/catalog/lib/item-inheritance.test.ts`

**Interfaces:**
- Consumes: `CatCategory` (Task 1), `ItemForm`, `itemFormFrom` (Task 2).
- Produces: `inheritedGroupIds(categories, categoryId): string[]`, `effectiveGroupIds(inherited, direct, excluded): string[]`, `changeCategory(form, categories, categoryId): ItemForm`, `toggleExcluded(form, groupId): ItemForm`, `toggleDirect(form, groupId): ItemForm`.

- [ ] **Step 1: Write the failing test**

```ts
// web/src/features/catalog/lib/item-inheritance.test.ts
import { describe, expect, it } from "bun:test";
import type { CatCategory } from "./catalog-model";
import { itemFormFrom } from "./forms";
import { changeCategory, effectiveGroupIds, inheritedGroupIds, toggleDirect, toggleExcluded } from "./item-inheritance";

const categories: CatCategory[] = [
  { id: "c-tea", name: "Trà", icon: null, displayOrder: 1, groupIds: ["g-ice", "g-sugar"] },
  { id: "c-cake", name: "Bánh", icon: null, displayOrder: 2, groupIds: ["g-sugar"] },
];

describe("item inheritance", () => {
  it("reads the category's groups", () => {
    expect(inheritedGroupIds(categories, "c-tea")).toEqual(["g-ice", "g-sugar"]);
    expect(inheritedGroupIds(categories, "missing")).toEqual([]);
  });

  it("offers inherited minus excluded, then direct groups", () => {
    expect(effectiveGroupIds(["g-ice", "g-sugar"], ["g-top", "g-sugar"], ["g-ice"])).toEqual(["g-sugar", "g-top"]);
  });

  it("drops exclusions the new category does not provide (ADR-059)", () => {
    const form = { ...itemFormFrom(null, "c-tea"), excludedGroupIds: ["g-ice", "g-sugar"] };
    expect(changeCategory(form, categories, "c-cake").excludedGroupIds).toEqual(["g-sugar"]);
  });

  it("removes a group from direct when it becomes excluded", () => {
    const form = { ...itemFormFrom(null, "c-tea"), directGroupIds: ["g-ice"] };
    const next = toggleExcluded(form, "g-ice");
    expect(next.excludedGroupIds).toEqual(["g-ice"]);
    expect(next.directGroupIds).toEqual([]);
    expect(toggleExcluded(next, "g-ice").excludedGroupIds).toEqual([]);
  });

  it("toggles a direct group", () => {
    const form = itemFormFrom(null, "c-tea");
    expect(toggleDirect(form, "g-top").directGroupIds).toEqual(["g-top"]);
    expect(toggleDirect(toggleDirect(form, "g-top"), "g-top").directGroupIds).toEqual([]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/catalog/lib/item-inheritance.test.ts`
Expected: FAIL, `Cannot find module './item-inheritance'`.

- [ ] **Step 3: Write the implementation**

```ts
// web/src/features/catalog/lib/item-inheritance.ts
import type { CatCategory } from "./catalog-model";
import type { ItemForm } from "./forms";

export function inheritedGroupIds(categories: readonly CatCategory[], categoryId: string): string[] {
  return categories.find((c) => c.id === categoryId)?.groupIds ?? [];
}

/** What the POS offers: inherited minus excluded, then direct groups not already present. */
export function effectiveGroupIds(
  inherited: readonly string[],
  direct: readonly string[],
  excluded: readonly string[],
): string[] {
  const out = inherited.filter((id) => !excluded.includes(id));
  for (const id of direct) if (!out.includes(id)) out.push(id);
  return out;
}

/** ADR-059: an exclusion survives only while the new category still provides the group. */
export function changeCategory(form: ItemForm, categories: readonly CatCategory[], categoryId: string): ItemForm {
  const inherited = inheritedGroupIds(categories, categoryId);
  return { ...form, categoryId, excludedGroupIds: form.excludedGroupIds.filter((id) => inherited.includes(id)) };
}

/** Excluding also detaches, because a group cannot be both direct and excluded (INVALID_INHERITANCE). */
export function toggleExcluded(form: ItemForm, groupId: string): ItemForm {
  if (form.excludedGroupIds.includes(groupId)) {
    return { ...form, excludedGroupIds: form.excludedGroupIds.filter((id) => id !== groupId) };
  }
  return {
    ...form,
    excludedGroupIds: [...form.excludedGroupIds, groupId],
    directGroupIds: form.directGroupIds.filter((id) => id !== groupId),
  };
}

export function toggleDirect(form: ItemForm, groupId: string): ItemForm {
  const on = form.directGroupIds.includes(groupId);
  return {
    ...form,
    directGroupIds: on ? form.directGroupIds.filter((id) => id !== groupId) : [...form.directGroupIds, groupId],
  };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && bun test src/features/catalog/lib/item-inheritance.test.ts`
Expected: PASS, 5 tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/catalog/lib/item-inheritance.ts web/src/features/catalog/lib/item-inheritance.test.ts
git commit -m "feat(web): item modifier-group inheritance helpers"
```

---

### Task 4: Save planners

**Files:**
- Create: `web/src/features/catalog/lib/save-plan.ts`
- Test: `web/src/features/catalog/lib/save-plan.test.ts`

**Interfaces:**
- Consumes: `Badge`, `Retirement`, `CatItem`, `CatCategory`, `CatGroup`, `Assignment`, `sameSet` (Task 1); `ItemForm`, `CategoryForm`, `GroupForm` (Task 2); `nameKey` (Task 2).
- Produces: `NEW_ID`, `NEW_OPTION_PREFIX`, `Command` (discriminated union on `type`), `Step { label: string; cmd: Command }`, `RetireKind = "item" | "category" | "group"`, `commandNeedsPin(cmd)`, `planNeedsPin(steps)`, `adoptRows(rows, existing, reserved?)`, `planItemSave(snapshot, form)`, `planCategorySave(snapshot, form)`, `planGroupSave(snapshot, form)`, `planRetire(kind, id, name, retirement)`, `planAssignments(groupId, groupName, current, next)`.

- [ ] **Step 1: Write the failing test**

```ts
// web/src/features/catalog/lib/save-plan.test.ts
import { describe, expect, it } from "bun:test";
import type { CatCategory, CatGroup, CatItem } from "./catalog-model";
import { categoryFormFrom, groupFormFrom, itemFormFrom, removeOptionRow, removeSizeRow } from "./forms";
import {
  NEW_ID,
  NEW_OPTION_PREFIX,
  planAssignments,
  planCategorySave,
  planGroupSave,
  planItemSave,
  planNeedsPin,
  planRetire,
  type Step,
} from "./save-plan";

const types = (steps: Step[]) => steps.map((s) => s.cmd.type);
const blob = new Blob(["x"], { type: "image/webp" });
const retire = { reason: "NO_LONGER_OFFERED" as const, note: "" };

const tea: CatItem = {
  id: "i-tea",
  categoryId: "c-tea",
  name: "Trà đào",
  code: null,
  badge: null,
  description: null,
  imageUrl: null,
  priceVnd: 45000,
  sizes: [],
  directGroupIds: [],
  excludedGroupIds: ["g-ice"],
};

const latte: CatItem = {
  id: "i-latte",
  categoryId: "c-coffee",
  name: "Latte",
  code: "lt",
  badge: "HOT",
  description: "Sữa tươi",
  imageUrl: "/media/catalog/a.webp",
  priceVnd: null,
  sizes: [
    { id: "s-m", name: "M", priceVnd: 39000, available: true },
    { id: "s-l", name: "L", priceVnd: 45000, available: true },
  ],
  directGroupIds: ["g-top"],
  excludedGroupIds: [],
};

describe("planItemSave", () => {
  it("plans nothing for an unchanged item", () => {
    expect(planItemSave(tea, itemFormFrom(tea, ""))).toEqual([]);
    expect(planItemSave(latte, itemFormFrom(latte, ""))).toEqual([]);
  });

  it("creates first and targets follow-up steps at the new id", () => {
    const form = {
      ...itemFormFrom(null, "c-tea"),
      name: " Trà vải ",
      priceVnd: 42000,
      code: "tv",
      image: { kind: "upload" as const, blob, previewUrl: "blob:x" },
      directGroupIds: ["g-top"],
    };
    const steps = planItemSave(null, form);
    expect(types(steps)).toEqual(["item.create", "item.details", "item.image.set", "item.groups"]);
    expect(steps[0].cmd).toEqual({ type: "item.create", categoryId: "c-tea", name: "Trà vải", priceVnd: 42000 });
    expect(steps.slice(1).every((s) => "itemId" in s.cmd && s.cmd.itemId === NEW_ID)).toBe(true);
  });

  it("creates a sized item with its sizes and nothing else when details are empty", () => {
    const form = {
      ...itemFormFrom(null, "c-tea"),
      name: "Trà sữa",
      mode: "sizes" as const,
      sizes: [
        { key: "a", id: null, name: " M ", priceVnd: 30000 },
        { key: "b", id: null, name: "L", priceVnd: 35000 },
      ],
    };
    const steps = planItemSave(null, form);
    expect(types(steps)).toEqual(["item.create"]);
    expect(steps[0].cmd).toMatchObject({
      sizes: [
        { name: "M", priceVnd: 30000 },
        { name: "L", priceVnd: 35000 },
      ],
    });
  });

  it("orders an edit so the backend never sees an invalid intermediate state", () => {
    let form = itemFormFrom(latte, "");
    form = {
      ...form,
      name: "Latte đá",
      categoryId: "c-tea",
      sizes: [
        { key: "s-m", id: "s-m", name: "Vừa", priceVnd: 41000 },
        { key: "s-l", id: "s-l", name: "L", priceVnd: 45000 },
        { key: "k", id: null, name: "XL", priceVnd: 52000 },
      ],
      description: "",
      image: { kind: "clear" },
      directGroupIds: [],
      excludedGroupIds: ["g-ice"],
    };
    form = removeSizeRow(form, "s-l", retire);
    expect(types(planItemSave(latte, form))).toEqual([
      "item.rename",
      "item.move",
      "size.rename",
      "size.reprice",
      "size.add",
      "size.retire",
      "item.details",
      "item.image.clear",
      "item.groups",
    ]);
  });

  it("reprices a single-price item and needs a PIN for it", () => {
    const steps = planItemSave(tea, { ...itemFormFrom(tea, ""), priceVnd: 47000 });
    expect(steps.map((s) => s.cmd)).toEqual([{ type: "item.reprice", itemId: "i-tea", priceVnd: 47000 }]);
    expect(planNeedsPin(steps)).toBe(true);
  });

  it("does not need a PIN for a rename", () => {
    expect(planNeedsPin(planItemSave(tea, { ...itemFormFrom(tea, ""), name: "Trà đào cam sả" }))).toBe(false);
  });

  it("adopts a new row the server already created when re-planning", () => {
    const form = {
      ...itemFormFrom(latte, ""),
      sizes: [...itemFormFrom(latte, "").sizes, { key: "k", id: null, name: "XL", priceVnd: 52000 }],
    };
    const refreshed: CatItem = { ...latte, sizes: [...latte.sizes, { id: "s-xl", name: "XL", priceVnd: 52000, available: true }] };
    expect(planItemSave(refreshed, form)).toEqual([]);
  });

  it("skips retiring a size the refreshed snapshot no longer has", () => {
    const form = removeSizeRow(itemFormFrom(latte, ""), "s-l", retire);
    const refreshed: CatItem = { ...latte, sizes: [latte.sizes[0]] };
    expect(planItemSave(refreshed, form)).toEqual([]);
  });

  it("clears an image only when one is stored", () => {
    expect(planItemSave(tea, { ...itemFormFrom(tea, ""), image: { kind: "clear" } })).toEqual([]);
  });
});

describe("planCategorySave", () => {
  const tea: CatCategory = { id: "c-tea", name: "Trà", icon: "leaf", displayOrder: 2, groupIds: ["g-ice"] };

  it("creates, then sets details and groups on the new id", () => {
    const steps = planCategorySave(null, { name: "Bánh", icon: "cake", displayOrder: 5, groupIds: ["g-ice"] });
    expect(types(steps)).toEqual(["category.create", "category.details", "category.groups"]);
    expect(steps[1].cmd).toEqual({ type: "category.details", categoryId: NEW_ID, icon: "cake", displayOrder: 5 });
  });

  it("creates alone when icon, order, and groups keep their defaults", () => {
    expect(types(planCategorySave(null, { name: "Bánh", icon: null, displayOrder: 0, groupIds: [] }))).toEqual([
      "category.create",
    ]);
  });

  it("plans only what changed", () => {
    expect(planCategorySave(tea, categoryFormFrom(tea, []))).toEqual([]);
    const steps = planCategorySave(tea, { ...categoryFormFrom(tea, []), displayOrder: 1 });
    expect(steps.map((s) => s.cmd)).toEqual([
      { type: "category.details", categoryId: "c-tea", icon: "leaf", displayOrder: 1 },
    ]);
  });
});

describe("planGroupSave", () => {
  const topping: CatGroup = {
    id: "g-top",
    name: "Topping",
    min: 0,
    max: 2,
    options: [
      { id: "o-pearl", name: "Trân châu", surchargeVnd: 10000, available: true },
      { id: "o-jelly", name: "Thạch", surchargeVnd: 8000, available: true },
    ],
    defaultOptionIds: ["o-jelly"],
  };

  it("creates a group in one step with default names", () => {
    const form = {
      ...groupFormFrom(null),
      name: "Mức đá",
      min: 1,
      max: 1,
      rows: [
        { key: "a", id: null, name: "100% đá", surchargeVnd: 0, isDefault: true },
        { key: "b", id: null, name: "Ít đá", surchargeVnd: 0, isDefault: false },
      ],
    };
    expect(planGroupSave(null, form).map((s) => s.cmd)).toEqual([
      {
        type: "group.create",
        name: "Mức đá",
        min: 1,
        max: 1,
        options: [
          { name: "100% đá", surchargeVnd: 0 },
          { name: "Ít đá", surchargeVnd: 0 },
        ],
        defaultOptionNames: ["100% đá"],
      },
    ]);
  });

  it("plans nothing for an unchanged group", () => {
    expect(planGroupSave(topping, groupFormFrom(topping))).toEqual([]);
  });

  it("adds options before the rule and retires after it", () => {
    let form = groupFormFrom(topping);
    form = {
      ...form,
      name: "Topping trà",
      max: 3,
      rows: [
        { ...form.rows[0], name: "Trân châu trắng", surchargeVnd: 12000 },
        form.rows[1],
        { key: "k", id: null, name: "Nha đam", surchargeVnd: 8000, isDefault: true },
      ],
    };
    form = removeOptionRow(form, "o-jelly", retire);
    const steps = planGroupSave(topping, form);
    expect(types(steps)).toEqual([
      "group.rename",
      "option.rename",
      "option.reprice",
      "option.add",
      "group.rule",
      "option.retire",
    ]);
    expect(steps[4].cmd).toEqual({
      type: "group.rule",
      groupId: "g-top",
      min: 0,
      max: 3,
      defaultOptionIds: [`${NEW_OPTION_PREFIX}Nha đam`],
    });
  });

  it("uses the adopted id for a default after re-planning", () => {
    const form = {
      ...groupFormFrom(topping),
      rows: [...groupFormFrom(topping).rows, { key: "k", id: null, name: "Nha đam", surchargeVnd: 8000, isDefault: true }],
    };
    const refreshed: CatGroup = {
      ...topping,
      options: [...topping.options, { id: "o-aloe", name: "Nha đam", surchargeVnd: 8000, available: true }],
    };
    expect(planGroupSave(refreshed, form).map((s) => s.cmd)).toEqual([
      { type: "group.rule", groupId: "g-top", min: 0, max: 2, defaultOptionIds: ["o-jelly", "o-aloe"] },
    ]);
  });
});

describe("single-command plans", () => {
  it("retires each entity kind with its own command", () => {
    expect(types(planRetire("item", "i", "Trà", retire))).toEqual(["item.retire"]);
    expect(types(planRetire("category", "c", "Trà", retire))).toEqual(["category.retire"]);
    expect(types(planRetire("group", "g", "Đá", retire))).toEqual(["group.retire"]);
    expect(planRetire("item", "i", "Trà", retire)[0].label).toBe("Ngừng bán Trà");
  });

  it("applies linker assignments only when they differ", () => {
    const current = { itemIds: ["a"], categoryIds: ["c"] };
    expect(planAssignments("g", "Đá", current, { itemIds: ["a"], categoryIds: ["c"] })).toEqual([]);
    expect(planAssignments("g", "Đá", current, { itemIds: ["a", "b"], categoryIds: [] }).map((s) => s.cmd)).toEqual([
      { type: "group.assignments", groupId: "g", itemIds: ["a", "b"], categoryIds: [] },
    ]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/catalog/lib/save-plan.test.ts`
Expected: FAIL, `Cannot find module './save-plan'`.

- [ ] **Step 3: Write the implementation**

```ts
// web/src/features/catalog/lib/save-plan.ts
import { sameSet, type Assignment, type Badge, type CatCategory, type CatGroup, type CatItem, type Retirement } from "./catalog-model";
import type { CategoryForm, GroupForm, ItemForm } from "./forms";
import { nameKey } from "./validation";

/** Stands for the id a create step in the same plan returns. */
export const NEW_ID = "$new";
/** Prefix of a default option that an `option.add` step in the same plan creates. */
export const NEW_OPTION_PREFIX = "new:";

export type Command =
  | { type: "item.create"; categoryId: string; name: string; priceVnd?: number; sizes?: { name: string; priceVnd: number }[] }
  | { type: "item.rename"; itemId: string; name: string }
  | { type: "item.move"; itemId: string; categoryId: string }
  | { type: "item.reprice"; itemId: string; priceVnd: number }
  | { type: "item.details"; itemId: string; code: string | null; badge: Badge | null; description: string | null }
  | { type: "item.image.set"; itemId: string; blob: Blob }
  | { type: "item.image.clear"; itemId: string }
  | { type: "item.groups"; itemId: string; directGroupIds: string[]; excludedGroupIds: string[] }
  | { type: "item.retire"; itemId: string; retirement: Retirement }
  | { type: "size.rename"; sizeId: string; name: string }
  | { type: "size.reprice"; sizeId: string; priceVnd: number }
  | { type: "size.add"; itemId: string; name: string; priceVnd: number }
  | { type: "size.retire"; sizeId: string; retirement: Retirement }
  | { type: "category.create"; name: string }
  | { type: "category.rename"; categoryId: string; name: string }
  | { type: "category.details"; categoryId: string; icon: string | null; displayOrder: number }
  | { type: "category.groups"; categoryId: string; groupIds: string[] }
  | { type: "category.retire"; categoryId: string; retirement: Retirement }
  | {
      type: "group.create";
      name: string;
      min: number;
      max: number;
      options: { name: string; surchargeVnd: number }[];
      defaultOptionNames: string[];
    }
  | { type: "group.rename"; groupId: string; name: string }
  | { type: "group.rule"; groupId: string; min: number; max: number; defaultOptionIds: string[] }
  | { type: "group.assignments"; groupId: string; itemIds: string[]; categoryIds: string[] }
  | { type: "group.retire"; groupId: string; retirement: Retirement }
  | { type: "option.rename"; optionId: string; name: string }
  | { type: "option.reprice"; optionId: string; surchargeVnd: number }
  | { type: "option.add"; groupId: string; name: string; surchargeVnd: number }
  | { type: "option.retire"; optionId: string; retirement: Retirement };

export interface Step {
  label: string;
  cmd: Command;
}

export type RetireKind = "item" | "category" | "group";

/** Commands whose endpoint requires `manager_pin` (catalog routes with RequireManagerPIN). */
const PIN_COMMANDS: ReadonlySet<Command["type"]> = new Set<Command["type"]>([
  "item.create",
  "item.reprice",
  "size.reprice",
  "size.add",
  "group.create",
  "option.reprice",
  "option.add",
]);

export function commandNeedsPin(cmd: Command): boolean {
  return PIN_COMMANDS.has(cmd.type);
}

export function planNeedsPin(steps: readonly Step[]): boolean {
  return steps.some((s) => commandNeedsPin(s.cmd));
}

/**
 * Gives an unsaved row the id of an existing entry with the same name that no
 * other row claims and that is not being retired. A re-plan after a partial
 * failure then treats a row whose add step already succeeded as saved,
 * instead of adding it twice (ADR-062).
 */
export function adoptRows<R extends { id: string | null; name: string }>(
  rows: readonly R[],
  existing: readonly { id: string; name: string }[],
  reserved: readonly string[] = [],
): R[] {
  const claimed = new Set<string>([...reserved, ...rows.flatMap((r) => (r.id ? [r.id] : []))]);
  const free = new Map<string, string>();
  for (const e of existing) if (!claimed.has(e.id)) free.set(nameKey(e.name), e.id);
  return rows.map((r) => {
    if (r.id) return r;
    const id = free.get(nameKey(r.name));
    if (!id) return r;
    free.delete(nameKey(r.name));
    return { ...r, id };
  });
}

interface Details {
  code: string | null;
  badge: Badge | null;
  description: string | null;
}

function detailsOf(form: ItemForm): Details {
  return { code: form.code.trim() || null, badge: form.badge, description: form.description.trim() || null };
}

function detailsStep(itemId: string, d: Details): Step {
  return { label: "Cập nhật mã, huy hiệu & mô tả", cmd: { type: "item.details", itemId, ...d } };
}

function groupsStep(itemId: string, form: ItemForm): Step {
  return {
    label: "Cập nhật nhóm topping",
    cmd: {
      type: "item.groups",
      itemId,
      directGroupIds: [...form.directGroupIds],
      excludedGroupIds: [...form.excludedGroupIds],
    },
  };
}

function imageStep(itemId: string, form: ItemForm, storedUrl: string | null): Step[] {
  if (form.image.kind === "upload") {
    return [{ label: "Tải ảnh lên", cmd: { type: "item.image.set", itemId, blob: form.image.blob } }];
  }
  if (form.image.kind === "clear" && storedUrl) return [{ label: "Gỡ ảnh", cmd: { type: "item.image.clear", itemId } }];
  return [];
}

function planSizes(snap: CatItem, form: ItemForm): Step[] {
  const current = new Map(snap.sizes.map((s) => [s.id, s]));
  const retiring = form.retiredSizes.filter((r) => current.has(r.id));
  const rows = adoptRows(form.sizes, snap.sizes, retiring.map((r) => r.id));
  const edits: Step[] = [];
  const adds: Step[] = [];
  for (const row of rows) {
    const name = row.name.trim();
    const cur = row.id ? current.get(row.id) : undefined;
    if (row.id && !cur) continue;
    if (!cur) {
      adds.push({ label: `Thêm kích cỡ ${name}`, cmd: { type: "size.add", itemId: snap.id, name, priceVnd: row.priceVnd } });
      continue;
    }
    if (name !== cur.name) edits.push({ label: `Đổi tên ${cur.name} thành ${name}`, cmd: { type: "size.rename", sizeId: cur.id, name } });
    if (row.priceVnd !== cur.priceVnd) {
      edits.push({ label: `Đổi giá ${name}`, cmd: { type: "size.reprice", sizeId: cur.id, priceVnd: row.priceVnd } });
    }
  }
  const retires = retiring.map(
    (r): Step => ({ label: `Ngừng bán ${r.name}`, cmd: { type: "size.retire", sizeId: r.id, retirement: r.retirement } }),
  );
  return [...edits, ...adds, ...retires];
}

// ponytail: the diff baseline is the snapshot taken when the modal opened, so a
// concurrent edit of the same entity on another terminal can be overwritten.
// One Manager edits the menu; add a version check to the commands if that changes.
/** Order: rename, move, prices and sizes, details, image, groups (spec §4.2). */
export function planItemSave(snap: CatItem | null, form: ItemForm): Step[] {
  const name = form.name.trim();
  const details = detailsOf(form);
  if (!snap) {
    const create: Command =
      form.mode === "single"
        ? { type: "item.create", categoryId: form.categoryId, name, priceVnd: form.priceVnd }
        : {
            type: "item.create",
            categoryId: form.categoryId,
            name,
            sizes: form.sizes.map((s) => ({ name: s.name.trim(), priceVnd: s.priceVnd })),
          };
    const steps: Step[] = [{ label: "Tạo món", cmd: create }];
    if (details.code || details.badge || details.description) steps.push(detailsStep(NEW_ID, details));
    steps.push(...imageStep(NEW_ID, form, null));
    if (form.directGroupIds.length > 0 || form.excludedGroupIds.length > 0) steps.push(groupsStep(NEW_ID, form));
    return steps;
  }

  const itemId = snap.id;
  const steps: Step[] = [];
  if (name !== snap.name) steps.push({ label: "Đổi tên món", cmd: { type: "item.rename", itemId, name } });
  if (form.categoryId !== snap.categoryId) {
    steps.push({ label: "Chuyển danh mục", cmd: { type: "item.move", itemId, categoryId: form.categoryId } });
  }
  if (form.mode === "single") {
    if (form.priceVnd !== snap.priceVnd) {
      steps.push({ label: "Đổi giá món", cmd: { type: "item.reprice", itemId, priceVnd: form.priceVnd } });
    }
  } else {
    steps.push(...planSizes(snap, form));
  }
  if (details.code !== snap.code || details.badge !== snap.badge || details.description !== snap.description) {
    steps.push(detailsStep(itemId, details));
  }
  steps.push(...imageStep(itemId, form, snap.imageUrl));
  if (!sameSet(form.directGroupIds, snap.directGroupIds) || !sameSet(form.excludedGroupIds, snap.excludedGroupIds)) {
    steps.push(groupsStep(itemId, form));
  }
  return steps;
}

export function planCategorySave(snap: CatCategory | null, form: CategoryForm): Step[] {
  const name = form.name.trim();
  const categoryId = snap?.id ?? NEW_ID;
  const steps: Step[] = [];
  if (!snap) steps.push({ label: "Tạo danh mục", cmd: { type: "category.create", name } });
  else if (name !== snap.name) steps.push({ label: "Đổi tên danh mục", cmd: { type: "category.rename", categoryId, name } });
  if (form.icon !== (snap?.icon ?? null) || form.displayOrder !== (snap?.displayOrder ?? 0)) {
    steps.push({
      label: "Cập nhật biểu tượng & thứ tự",
      cmd: { type: "category.details", categoryId, icon: form.icon, displayOrder: form.displayOrder },
    });
  }
  if (!sameSet(form.groupIds, snap?.groupIds ?? [])) {
    steps.push({ label: "Cập nhật nhóm mặc định", cmd: { type: "category.groups", categoryId, groupIds: [...form.groupIds] } });
  }
  return steps;
}

/** Order: rename, option edits, adds, selection rule, retirements (spec §4.2). */
export function planGroupSave(snap: CatGroup | null, form: GroupForm): Step[] {
  const name = form.name.trim();
  if (!snap) {
    return [
      {
        label: "Tạo nhóm topping",
        cmd: {
          type: "group.create",
          name,
          min: form.min,
          max: form.max,
          options: form.rows.map((r) => ({ name: r.name.trim(), surchargeVnd: r.surchargeVnd })),
          defaultOptionNames: form.rows.filter((r) => r.isDefault).map((r) => r.name.trim()),
        },
      },
    ];
  }

  const groupId = snap.id;
  const current = new Map(snap.options.map((o) => [o.id, o]));
  const retiring = form.retiredOptions.filter((r) => current.has(r.id));
  const rows = adoptRows(form.rows, snap.options, retiring.map((r) => r.id));
  const steps: Step[] = [];
  const adds: Step[] = [];
  if (name !== snap.name) steps.push({ label: "Đổi tên nhóm", cmd: { type: "group.rename", groupId, name } });
  for (const row of rows) {
    const optName = row.name.trim();
    const cur = row.id ? current.get(row.id) : undefined;
    if (row.id && !cur) continue;
    if (!cur) {
      adds.push({ label: `Thêm ${optName}`, cmd: { type: "option.add", groupId, name: optName, surchargeVnd: row.surchargeVnd } });
      continue;
    }
    if (optName !== cur.name) {
      steps.push({ label: `Đổi tên ${cur.name} thành ${optName}`, cmd: { type: "option.rename", optionId: cur.id, name: optName } });
    }
    if (row.surchargeVnd !== cur.surchargeVnd) {
      steps.push({ label: `Đổi giá ${optName}`, cmd: { type: "option.reprice", optionId: cur.id, surchargeVnd: row.surchargeVnd } });
    }
  }
  steps.push(...adds);
  const defaults = rows.filter((r) => r.isDefault).map((r) => r.id ?? `${NEW_OPTION_PREFIX}${r.name.trim()}`);
  if (form.min !== snap.min || form.max !== snap.max || !sameSet(defaults, snap.defaultOptionIds)) {
    steps.push({
      label: "Cập nhật quy tắc chọn",
      cmd: { type: "group.rule", groupId, min: form.min, max: form.max, defaultOptionIds: defaults },
    });
  }
  steps.push(
    ...retiring.map(
      (r): Step => ({ label: `Ngừng bán ${r.name}`, cmd: { type: "option.retire", optionId: r.id, retirement: r.retirement } }),
    ),
  );
  return steps;
}

export function planRetire(kind: RetireKind, id: string, name: string, retirement: Retirement): Step[] {
  const label = `Ngừng bán ${name}`;
  if (kind === "item") return [{ label, cmd: { type: "item.retire", itemId: id, retirement } }];
  if (kind === "category") return [{ label, cmd: { type: "category.retire", categoryId: id, retirement } }];
  return [{ label, cmd: { type: "group.retire", groupId: id, retirement } }];
}

export function planAssignments(groupId: string, groupName: string, current: Assignment, next: Assignment): Step[] {
  if (sameSet(current.itemIds, next.itemIds) && sameSet(current.categoryIds, next.categoryIds)) return [];
  return [
    {
      label: `Áp dụng ${groupName}`,
      cmd: { type: "group.assignments", groupId, itemIds: [...next.itemIds], categoryIds: [...next.categoryIds] },
    },
  ];
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && bun test src/features/catalog/lib/save-plan.test.ts`
Expected: PASS, 18 tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/catalog/lib/save-plan.ts web/src/features/catalog/lib/save-plan.test.ts
git commit -m "feat(web): plan catalog form saves as ordered command lists"
```

---

### Task 5: Plan runner, command executor, and hooks

**Files:**
- Create: `web/src/features/catalog/lib/run-plan.ts`
- Create: `web/src/features/catalog/api/execute-command.ts`
- Create: `web/src/features/catalog/api/use-catalog-admin.ts`
- Test: `web/src/features/catalog/lib/run-plan.test.ts`

**Interfaces:**
- Consumes: `Command`, `Step`, `NEW_ID`, `NEW_OPTION_PREFIX`, `planNeedsPin`, `planRetire`, `RetireKind` (Task 4); `toCatalogModel`, `CatalogModel`, `Retirement` (Task 1); `messageForError` from `@/lib/error-messages`; `useManagerApprovalStore` from `@/stores/use-manager-approval-store`; generated catalog functions and hooks.
- Produces:
  - `run-plan.ts`: `StepStatus`, `StepResult { label; type: Command["type"]; status; error? }`, `Created { itemId?; categoryId?; optionIds: Record<string, string> }`, `ExecResult`, `Executor = (cmd, pin) => Promise<ExecResult>`, `RunOutcome { ok; results; created }`, `resolveCommand(cmd, created)`, `runPlan(steps, exec, pin)`.
  - `execute-command.ts`: `executeCommand: Executor`.
  - `use-catalog-admin.ts`: `useCatalogModel(): { model; isPending; error; refetch }`, `useInvalidateCatalog()`, `SaveOutcome = RunOutcome & { cancelled: boolean }`, `useCatalogSave(): { save(steps): Promise<SaveOutcome>; isSaving; results; clearResults }`, `useRetireEntity(): (kind, id, name, retirement) => Promise<string | null>`.

- [ ] **Step 1: Write the failing test**

```ts
// web/src/features/catalog/lib/run-plan.test.ts
import { describe, expect, it } from "bun:test";
import { messageForError } from "@/lib/error-messages";
import { ApiError } from "@/lib/unwrap";
import { resolveCommand, runPlan, type Executor } from "./run-plan";
import { NEW_ID, NEW_OPTION_PREFIX, type Command, type Step } from "./save-plan";

const create: Step = { label: "Tạo món", cmd: { type: "item.create", categoryId: "c", name: "A", priceVnd: 1000 } };
const groups: Step = {
  label: "Cập nhật nhóm topping",
  cmd: { type: "item.groups", itemId: NEW_ID, directGroupIds: ["g"], excludedGroupIds: [] },
};
const details: Step = {
  label: "Cập nhật mã, huy hiệu & mô tả",
  cmd: { type: "item.details", itemId: NEW_ID, code: "a", badge: null, description: null },
};

describe("runPlan", () => {
  it("runs steps in order, resolving NEW_ID from the create step", async () => {
    const seen: Command[] = [];
    const exec: Executor = async (cmd) => {
      seen.push(cmd);
      if (cmd.type === "item.create") return { created: "item", id: "i-1" };
    };
    const out = await runPlan([create, groups], exec, null);
    expect(out.ok).toBe(true);
    expect(out.created.itemId).toBe("i-1");
    expect(seen[1]).toEqual({ type: "item.groups", itemId: "i-1", directGroupIds: ["g"], excludedGroupIds: [] });
    expect(out.results.map((r) => r.status)).toEqual(["done", "done"]);
  });

  it("stops at the first failure and leaves later steps pending", async () => {
    const err = new ApiError(409, "CATALOG_CODE_CONFLICT", "code taken");
    const exec: Executor = async (cmd) => {
      if (cmd.type === "item.create") return { created: "item", id: "i-1" };
      if (cmd.type === "item.details") throw err;
    };
    const out = await runPlan([create, details, groups], exec, null);
    expect(out.ok).toBe(false);
    expect(out.results.map((r) => r.status)).toEqual(["done", "failed", "pending"]);
    expect(out.results[1].error).toBe(messageForError(err));
    expect(out.results[1].type).toBe("item.details");
    expect(out.created.itemId).toBe("i-1");
  });

  it("passes the PIN to every step", async () => {
    const pins: (string | null)[] = [];
    const exec: Executor = async (cmd, pin) => {
      pins.push(pin);
      if (cmd.type === "item.create") return { created: "item", id: "i-1" };
    };
    await runPlan([create, groups], exec, "1234");
    expect(pins).toEqual(["1234", "1234"]);
  });
});

describe("resolveCommand", () => {
  it("resolves options created earlier in the plan", () => {
    const rule: Command = {
      type: "group.rule",
      groupId: "g",
      min: 0,
      max: 2,
      defaultOptionIds: ["o-1", `${NEW_OPTION_PREFIX}Nha đam`],
    };
    expect(resolveCommand(rule, { optionIds: { "Nha đam": "o-2" } })).toEqual({ ...rule, defaultOptionIds: ["o-1", "o-2"] });
  });

  it("resolves a new category id and leaves real ids alone", () => {
    const cmd: Command = { type: "category.groups", categoryId: NEW_ID, groupIds: [] };
    expect(resolveCommand(cmd, { categoryId: "c-9", optionIds: {} })).toEqual({ ...cmd, categoryId: "c-9" });
    const move: Command = { type: "item.move", itemId: "i", categoryId: "c-1" };
    expect(resolveCommand(move, { optionIds: {} })).toEqual(move);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/catalog/lib/run-plan.test.ts`
Expected: FAIL, `Cannot find module './run-plan'`.

- [ ] **Step 3: Write `run-plan.ts`**

```ts
// web/src/features/catalog/lib/run-plan.ts
import { messageForError } from "@/lib/error-messages";
import { NEW_ID, NEW_OPTION_PREFIX, type Command, type Step } from "./save-plan";

export type StepStatus = "done" | "failed" | "pending";

export interface StepResult {
  label: string;
  type: Command["type"];
  status: StepStatus;
  error?: string;
}

/** Ids returned by create steps, used to resolve NEW_ID and new-option refs. */
export interface Created {
  itemId?: string;
  categoryId?: string;
  optionIds: Record<string, string>;
}

export type ExecResult =
  | { created: "item" | "category"; id: string }
  | { created: "option"; id: string; name: string }
  | void;

export type Executor = (cmd: Command, pin: string | null) => Promise<ExecResult>;

export interface RunOutcome {
  ok: boolean;
  results: StepResult[];
  created: Created;
}

function need(id: string | undefined): string {
  if (!id) throw new Error("A step referenced an id its create step did not return");
  return id;
}

export function resolveCommand(cmd: Command, created: Created): Command {
  let out: Command = cmd;
  if ("itemId" in out && out.itemId === NEW_ID) out = { ...out, itemId: need(created.itemId) } as Command;
  if ("categoryId" in out && out.categoryId === NEW_ID) out = { ...out, categoryId: need(created.categoryId) } as Command;
  if (out.type === "group.rule") {
    out = {
      ...out,
      defaultOptionIds: out.defaultOptionIds.map((id) =>
        id.startsWith(NEW_OPTION_PREFIX) ? need(created.optionIds[id.slice(NEW_OPTION_PREFIX.length)]) : id,
      ),
    };
  }
  return out;
}

/** Runs steps in order and stops at the first failure; later steps stay pending. */
export async function runPlan(steps: readonly Step[], exec: Executor, pin: string | null): Promise<RunOutcome> {
  const created: Created = { optionIds: {} };
  const results: StepResult[] = steps.map((s) => ({ label: s.label, type: s.cmd.type, status: "pending" }));
  for (const [i, step] of steps.entries()) {
    try {
      const res = await exec(resolveCommand(step.cmd, created), pin);
      if (res?.created === "item") created.itemId = res.id;
      else if (res?.created === "category") created.categoryId = res.id;
      else if (res?.created === "option") created.optionIds[res.name] = res.id;
      results[i] = { ...results[i], status: "done" };
    } catch (err) {
      results[i] = { ...results[i], status: "failed", error: messageForError(err) };
      return { ok: false, results, created };
    }
  }
  return { ok: true, results, created };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && bun test src/features/catalog/lib/run-plan.test.ts`
Expected: PASS, 5 tests.

- [ ] **Step 5: Write `execute-command.ts`**

```ts
// web/src/features/catalog/api/execute-command.ts
import {
  deleteCatalogItemsItemIdImage,
  patchCatalogCategoriesCategoryIdDetails,
  patchCatalogCategoriesCategoryIdName,
  patchCatalogItemsItemIdCategory,
  patchCatalogItemsItemIdDetails,
  patchCatalogItemsItemIdName,
  patchCatalogItemsItemIdPrice,
  patchCatalogModifierGroupsGroupIdName,
  patchCatalogModifierOptionsOptionIdName,
  patchCatalogModifierOptionsOptionIdPrice,
  patchCatalogSizesSizeIdName,
  patchCatalogSizesSizeIdPrice,
  postCatalogCategories,
  postCatalogCategoriesCategoryIdRetirement,
  postCatalogItems,
  postCatalogItemsItemIdRetirement,
  postCatalogItemsItemIdSizes,
  postCatalogModifierGroups,
  postCatalogModifierGroupsGroupIdOptions,
  postCatalogModifierGroupsGroupIdRetirement,
  postCatalogModifierOptionsOptionIdRetirement,
  postCatalogSizesSizeIdRetirement,
  putCatalogCategoriesCategoryIdModifierGroups,
  putCatalogItemsItemIdImage,
  putCatalogItemsItemIdModifierGroups,
  putCatalogModifierGroupsGroupIdAssignments,
  putCatalogModifierGroupsGroupIdSelectionRule,
} from "@/api/generated/endpoints/catalog/catalog";
import type { CatalogSetCategoryDetailsRequest, CatalogSetItemDetailsRequest } from "@/api/generated/models";
import { newRequestId } from "@/lib/command";
import { unwrap, unwrapNullable } from "@/lib/unwrap";
import type { Retirement } from "../lib/catalog-model";
import type { Executor } from "../lib/run-plan";

function retireBody(request_id: string, r: Retirement) {
  return { request_id, reason: r.reason, note: r.note.trim() };
}

/**
 * Maps one planned command to its endpoint. Each call is its own intent with a
 * fresh request_id; a retry re-plans against refreshed state rather than
 * replaying (ADR-062). Commands that return nothing useful use unwrapNullable,
 * so an empty `data` is not mistaken for a failure.
 */
export const executeCommand: Executor = async (cmd, pin) => {
  const request_id = newRequestId();
  const manager_pin = pin ?? undefined;
  switch (cmd.type) {
    case "item.create": {
      const item = unwrap(
        await postCatalogItems({
          request_id,
          manager_pin,
          category_id: cmd.categoryId,
          name: cmd.name,
          price_vnd: cmd.priceVnd,
          sizes: cmd.sizes?.map((s) => ({ name: s.name, price_vnd: s.priceVnd })),
        }),
      );
      return { created: "item", id: item.id ?? "" };
    }
    case "item.rename":
      unwrapNullable(await patchCatalogItemsItemIdName(cmd.itemId, { request_id, name: cmd.name }));
      return;
    case "item.move":
      unwrapNullable(await patchCatalogItemsItemIdCategory(cmd.itemId, { request_id, category_id: cmd.categoryId }));
      return;
    case "item.reprice":
      unwrapNullable(await patchCatalogItemsItemIdPrice(cmd.itemId, { request_id, manager_pin, price_vnd: cmd.priceVnd }));
      return;
    case "item.details": {
      // The server requires all three keys and reads null as "clear"; the
      // generated type has no null, hence the cast.
      const body = { request_id, code: cmd.code, badge: cmd.badge, description: cmd.description };
      unwrapNullable(await patchCatalogItemsItemIdDetails(cmd.itemId, body as unknown as CatalogSetItemDetailsRequest));
      return;
    }
    case "item.image.set":
      unwrapNullable(await putCatalogItemsItemIdImage(cmd.itemId, { request_id, file: cmd.blob }));
      return;
    case "item.image.clear":
      unwrapNullable(await deleteCatalogItemsItemIdImage(cmd.itemId, { request_id }));
      return;
    case "item.groups":
      unwrapNullable(
        await putCatalogItemsItemIdModifierGroups(cmd.itemId, {
          request_id,
          direct_group_ids: cmd.directGroupIds,
          excluded_group_ids: cmd.excludedGroupIds,
        }),
      );
      return;
    case "item.retire":
      unwrapNullable(await postCatalogItemsItemIdRetirement(cmd.itemId, retireBody(request_id, cmd.retirement)));
      return;
    case "size.rename":
      unwrapNullable(await patchCatalogSizesSizeIdName(cmd.sizeId, { request_id, name: cmd.name }));
      return;
    case "size.reprice":
      unwrapNullable(await patchCatalogSizesSizeIdPrice(cmd.sizeId, { request_id, manager_pin, price_vnd: cmd.priceVnd }));
      return;
    case "size.add":
      unwrapNullable(
        await postCatalogItemsItemIdSizes(cmd.itemId, { request_id, manager_pin, name: cmd.name, price_vnd: cmd.priceVnd }),
      );
      return;
    case "size.retire":
      unwrapNullable(await postCatalogSizesSizeIdRetirement(cmd.sizeId, retireBody(request_id, cmd.retirement)));
      return;
    case "category.create": {
      const category = unwrap(await postCatalogCategories({ request_id, name: cmd.name }));
      return { created: "category", id: category.id ?? "" };
    }
    case "category.rename":
      unwrapNullable(await patchCatalogCategoriesCategoryIdName(cmd.categoryId, { request_id, name: cmd.name }));
      return;
    case "category.details": {
      const body = { request_id, icon: cmd.icon, display_order: cmd.displayOrder };
      unwrapNullable(
        await patchCatalogCategoriesCategoryIdDetails(cmd.categoryId, body as unknown as CatalogSetCategoryDetailsRequest),
      );
      return;
    }
    case "category.groups":
      unwrapNullable(await putCatalogCategoriesCategoryIdModifierGroups(cmd.categoryId, { request_id, group_ids: cmd.groupIds }));
      return;
    case "category.retire":
      unwrapNullable(await postCatalogCategoriesCategoryIdRetirement(cmd.categoryId, retireBody(request_id, cmd.retirement)));
      return;
    case "group.create":
      unwrapNullable(
        await postCatalogModifierGroups({
          request_id,
          manager_pin,
          name: cmd.name,
          min_selections: cmd.min,
          max_selections: cmd.max,
          options: cmd.options.map((o) => ({ name: o.name, surcharge_vnd: o.surchargeVnd })),
          default_option_names: cmd.defaultOptionNames,
        }),
      );
      return;
    case "group.rename":
      unwrapNullable(await patchCatalogModifierGroupsGroupIdName(cmd.groupId, { request_id, name: cmd.name }));
      return;
    case "group.rule":
      unwrapNullable(
        await putCatalogModifierGroupsGroupIdSelectionRule(cmd.groupId, {
          request_id,
          min_selections: cmd.min,
          max_selections: cmd.max,
          default_option_ids: cmd.defaultOptionIds,
        }),
      );
      return;
    case "group.assignments":
      unwrapNullable(
        await putCatalogModifierGroupsGroupIdAssignments(cmd.groupId, {
          request_id,
          item_ids: cmd.itemIds,
          category_ids: cmd.categoryIds,
        }),
      );
      return;
    case "group.retire":
      unwrapNullable(await postCatalogModifierGroupsGroupIdRetirement(cmd.groupId, retireBody(request_id, cmd.retirement)));
      return;
    case "option.rename":
      unwrapNullable(await patchCatalogModifierOptionsOptionIdName(cmd.optionId, { request_id, name: cmd.name }));
      return;
    case "option.reprice":
      unwrapNullable(
        await patchCatalogModifierOptionsOptionIdPrice(cmd.optionId, { request_id, manager_pin, surcharge_vnd: cmd.surchargeVnd }),
      );
      return;
    case "option.add": {
      const option = unwrap(
        await postCatalogModifierGroupsGroupIdOptions(cmd.groupId, {
          request_id,
          manager_pin,
          name: cmd.name,
          surcharge_vnd: cmd.surchargeVnd,
        }),
      );
      return { created: "option", id: option.id ?? "", name: cmd.name };
    }
    case "option.retire":
      unwrapNullable(await postCatalogModifierOptionsOptionIdRetirement(cmd.optionId, retireBody(request_id, cmd.retirement)));
      return;
  }
};
```

If `bunx tsc -b` reports that a generated function takes a different body type than shown (for example `patchCatalogSizesSizeIdPrice` expecting `CatalogRepriceRequest`), open its signature in `web/src/api/generated/endpoints/catalog/catalog.ts` and match the field names there. The field names above were copied from `web/src/api/generated/models/`.

- [ ] **Step 6: Write `use-catalog-admin.ts`**

```ts
// web/src/features/catalog/api/use-catalog-admin.ts
import { useCallback, useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  getGetCatalogMenuAvailabilityQueryKey,
  getGetCatalogMenuManageQueryKey,
  getGetCatalogMenuSellableQueryKey,
  getGetCatalogModifierGroupsQueryKey,
  useGetCatalogMenuManage,
  useGetCatalogModifierGroups,
} from "@/api/generated/endpoints/catalog/catalog";
import type {
  CatalogManagementMenuResponse,
  CatalogManagementModifierGroupResponse,
  GetCatalogModifierGroups200,
} from "@/api/generated/models";
import { playErrorBuzz, playSuccessChirp } from "@/lib/sound";
import { ApiError, unwrap, unwrapNullable } from "@/lib/unwrap";
import { useManagerApprovalStore } from "@/stores/use-manager-approval-store";
import { toCatalogModel, type CatalogModel, type Retirement } from "../lib/catalog-model";
import { runPlan, type RunOutcome, type StepResult } from "../lib/run-plan";
import { planNeedsPin, planRetire, type RetireKind, type Step } from "../lib/save-plan";
import { executeCommand } from "./execute-command";

export interface CatalogQuery {
  model: CatalogModel;
  isPending: boolean;
  error: ApiError | null;
  refetch: () => void;
}

/** A Go nil slice arrives as `data: null`, so the group list tolerates it. */
const selectGroups = (res: GetCatalogModifierGroups200) => unwrapNullable(res) ?? [];

export function useCatalogModel(): CatalogQuery {
  const menu = useGetCatalogMenuManage<CatalogManagementMenuResponse, ApiError>({ query: { select: unwrap } });
  const groups = useGetCatalogModifierGroups<CatalogManagementModifierGroupResponse[], ApiError>({
    query: { select: selectGroups },
  });
  const model = useMemo(() => toCatalogModel(menu.data, groups.data), [menu.data, groups.data]);
  return {
    model,
    isPending: menu.isPending || groups.isPending,
    error: (!menu.data ? menu.error : null) ?? (!groups.data ? groups.error : null),
    refetch: () => {
      void menu.refetch();
      void groups.refetch();
    },
  };
}

/** Every screen that shows catalog data: this tab, the POS menu, and the 9a tab. */
export function useInvalidateCatalog(): () => Promise<void> {
  const queryClient = useQueryClient();
  return useCallback(async () => {
    await Promise.all(
      [
        getGetCatalogMenuManageQueryKey(),
        getGetCatalogModifierGroupsQueryKey(),
        getGetCatalogMenuSellableQueryKey(),
        getGetCatalogMenuAvailabilityQueryKey(),
      ].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
    );
  }, [queryClient]);
}

export type SaveOutcome = RunOutcome & { cancelled: boolean };

const EMPTY_OUTCOME: RunOutcome = { ok: true, results: [], created: { optionIds: {} } };

/**
 * Runs a planned save (ADR-062): asks for the Manager PIN once when any step
 * needs it, runs the steps in order, then refetches so a retry re-plans
 * against fresh state. The PIN lives only in this call.
 */
export function useCatalogSave() {
  const invalidate = useInvalidateCatalog();
  const [isSaving, setIsSaving] = useState(false);
  const [results, setResults] = useState<StepResult[] | null>(null);

  const save = useCallback(
    async (steps: Step[]): Promise<SaveOutcome> => {
      if (steps.length === 0) return { ...EMPTY_OUTCOME, cancelled: false };
      let pin: string | null = null;
      if (planNeedsPin(steps)) {
        try {
          const creds = await useManagerApprovalStore.getState().promptApproval({
            title: "Xác nhận thay đổi giá",
            description: "Thay đổi này có giá bán. Nhập PIN Quản lý để lưu.",
            confirmLabel: "Xác nhận & Lưu",
          });
          pin = creds.managerPin;
        } catch {
          return { ...EMPTY_OUTCOME, ok: false, cancelled: true };
        }
      }
      setIsSaving(true);
      setResults(null);
      try {
        const outcome = await runPlan(steps, executeCommand, pin);
        setResults(outcome.results);
        if (outcome.ok) playSuccessChirp();
        else playErrorBuzz();
        return { ...outcome, cancelled: false };
      } finally {
        await invalidate();
        setIsSaving(false);
      }
    },
    [invalidate],
  );

  return { save, isSaving, results, clearResults: useCallback(() => setResults(null), []) };
}

/** Retires an item, category, or group; resolves to an error message or null. */
export function useRetireEntity() {
  const { save } = useCatalogSave();
  return useCallback(
    async (kind: RetireKind, id: string, name: string, retirement: Retirement): Promise<string | null> => {
      const outcome = await save(planRetire(kind, id, name, retirement));
      if (outcome.ok) return null;
      return outcome.results.find((r) => r.status === "failed")?.error ?? "Không thể ngừng bán";
    },
    [save],
  );
}
```

- [ ] **Step 7: Type-check**

Run: `cd web && bunx tsc -b`
Expected: exit 0. Fix any generated-signature mismatch as noted in Step 5.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/catalog/lib/run-plan.ts web/src/features/catalog/lib/run-plan.test.ts web/src/features/catalog/api/execute-command.ts web/src/features/catalog/api/use-catalog-admin.ts
git commit -m "feat(web): run catalog save plans against the catalog API"
```

---

### Task 6: Image resize

**Files:**
- Create: `web/src/features/catalog/lib/image-resize.ts`
- Test: `web/src/features/catalog/lib/image-resize.test.ts`

**Interfaces:**
- Consumes: `ApiError` from `@/lib/unwrap`; `ERROR_MESSAGES` from `@/lib/error-messages` (keys `INVALID_IMAGE`, `IMAGE_TOO_LARGE` exist since BA-1).
- Produces: `MAX_IMAGE_EDGE = 800`, `MAX_IMAGE_BYTES = 1_048_576`, `QUALITY_STEPS`, `fitWithin(width, height, maxEdge?)`, `resizeToWebp(file: Blob): Promise<Blob>`.

- [ ] **Step 1: Write the failing test**

```ts
// web/src/features/catalog/lib/image-resize.test.ts
import { describe, expect, it } from "bun:test";
import { fitWithin, MAX_IMAGE_BYTES, MAX_IMAGE_EDGE } from "./image-resize";

describe("fitWithin", () => {
  it("scales the long edge down to the cap and keeps the ratio", () => {
    expect(fitWithin(1600, 1200)).toEqual({ width: 800, height: 600 });
    expect(fitWithin(1000, 3000)).toEqual({ width: 267, height: 800 });
  });

  it("never upscales", () => {
    expect(fitWithin(400, 300)).toEqual({ width: 400, height: 300 });
  });

  it("uses the BA-1 limits", () => {
    expect(MAX_IMAGE_EDGE).toBe(800);
    expect(MAX_IMAGE_BYTES).toBe(1_048_576);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/catalog/lib/image-resize.test.ts`
Expected: FAIL, `Cannot find module './image-resize'`.

- [ ] **Step 3: Write the implementation**

```ts
// web/src/features/catalog/lib/image-resize.ts
import { ERROR_MESSAGES } from "@/lib/error-messages";
import { ApiError } from "@/lib/unwrap";

export const MAX_IMAGE_EDGE = 800;
/** The server's cap (BA-1 §3.2). */
export const MAX_IMAGE_BYTES = 1_048_576;
export const QUALITY_STEPS = [0.85, 0.75, 0.65, 0.55, 0.45] as const;

export function fitWithin(width: number, height: number, maxEdge = MAX_IMAGE_EDGE): { width: number; height: number } {
  const scale = Math.min(1, maxEdge / Math.max(width, height));
  return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)) };
}

/**
 * Shrinks a picked image in the browser so no image library enters the Go
 * binary. A browser without WebP encoding returns PNG, which the server also
 * accepts.
 */
export async function resizeToWebp(file: Blob): Promise<Blob> {
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file);
  } catch {
    throw new ApiError(0, "INVALID_IMAGE", ERROR_MESSAGES.INVALID_IMAGE);
  }
  const { width, height } = fitWithin(bitmap.width, bitmap.height);
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new ApiError(0, "INVALID_IMAGE", ERROR_MESSAGES.INVALID_IMAGE);
  ctx.drawImage(bitmap, 0, 0, width, height);
  bitmap.close();
  for (const quality of QUALITY_STEPS) {
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/webp", quality));
    if (!blob) break;
    if (blob.size <= MAX_IMAGE_BYTES) return blob;
  }
  throw new ApiError(0, "IMAGE_TOO_LARGE", ERROR_MESSAGES.IMAGE_TOO_LARGE);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && bun test src/features/catalog/lib/image-resize.test.ts`
Expected: PASS, 3 tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/catalog/lib/image-resize.ts web/src/features/catalog/lib/image-resize.test.ts
git commit -m "feat(web): resize item images in the browser before upload"
```

---

### Task 7: Sellable preview

**Files:**
- Create: `web/src/features/catalog/lib/sellable-preview.ts`
- Test: `web/src/features/catalog/lib/sellable-preview.test.ts`

**Interfaces:**
- Consumes: `CatItem`, `CatalogModel`, `Assignment` (Task 1); `effectiveGroupIds` (Task 3); `CatalogSellableItemResponse`.
- Produces: `PreviewOverride = Assignment & { groupId: string }`, `toSellablePreview(item, model, override?): CatalogSellableItemResponse`.

- [ ] **Step 1: Write the failing test**

```ts
// web/src/features/catalog/lib/sellable-preview.test.ts
import { describe, expect, it } from "bun:test";
import type { CatalogModel, CatItem } from "./catalog-model";
import { toSellablePreview } from "./sellable-preview";

const tea: CatItem = {
  id: "i-tea",
  categoryId: "c-tea",
  name: "Trà đào",
  code: null,
  badge: null,
  description: null,
  imageUrl: null,
  priceVnd: 45000,
  sizes: [],
  directGroupIds: ["g-top"],
  excludedGroupIds: [],
};

const latte: CatItem = {
  ...tea,
  id: "i-latte",
  name: "Latte",
  priceVnd: null,
  sizes: [
    { id: "s-m", name: "M", priceVnd: 39000, available: true },
    { id: "s-l", name: "L", priceVnd: 45000, available: false },
  ],
  directGroupIds: [],
  excludedGroupIds: ["g-ice"],
};

const model: CatalogModel = {
  categories: [{ id: "c-tea", name: "Trà", icon: null, displayOrder: 1, groupIds: ["g-ice"] }],
  items: [tea, latte],
  groups: [
    {
      id: "g-ice",
      name: "Mức đá",
      min: 1,
      max: 1,
      options: [
        { id: "o-full", name: "100% đá", surchargeVnd: 0, available: true },
        { id: "o-less", name: "Ít đá", surchargeVnd: 0, available: false },
      ],
      defaultOptionIds: ["o-full"],
    },
    {
      id: "g-top",
      name: "Topping",
      min: 0,
      max: 2,
      options: [{ id: "o-pearl", name: "Trân châu", surchargeVnd: 10000, available: true }],
      defaultOptionIds: [],
    },
  ],
};

describe("toSellablePreview", () => {
  it("offers inherited then direct groups with only available options", () => {
    const preview = toSellablePreview(tea, model);
    expect(preview.modifier_groups?.map((g) => g.id)).toEqual(["g-ice", "g-top"]);
    expect(preview.modifier_groups?.[0].options?.map((o) => o.id)).toEqual(["o-full"]);
    expect(preview.price_vnd).toBe(45000);
    expect(preview.sizes).toEqual([]);
  });

  it("drops excluded groups and unavailable sizes", () => {
    const preview = toSellablePreview(latte, model);
    expect(preview.modifier_groups).toEqual([]);
    expect(preview.sizes).toEqual([{ id: "s-m", name: "M", price_vnd: 39000 }]);
    expect(preview.price_vnd).toBeUndefined();
  });

  it("applies the linker's pending assignment for its group", () => {
    const off = toSellablePreview(tea, model, { groupId: "g-ice", itemIds: [], categoryIds: [] });
    expect(off.modifier_groups?.map((g) => g.id)).toEqual(["g-top"]);
    const direct = toSellablePreview(latte, model, { groupId: "g-top", itemIds: ["i-latte"], categoryIds: [] });
    expect(direct.modifier_groups?.map((g) => g.id)).toEqual(["g-top"]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/catalog/lib/sellable-preview.test.ts`
Expected: FAIL, `Cannot find module './sellable-preview'`.

- [ ] **Step 3: Write the implementation**

```ts
// web/src/features/catalog/lib/sellable-preview.ts
import type { CatalogSellableItemResponse } from "@/api/generated/models";
import type { Assignment, CatalogModel, CatItem } from "./catalog-model";
import { effectiveGroupIds } from "./item-inheritance";

/** The Batch Linker's unsaved state for one group. */
export type PreviewOverride = Assignment & { groupId: string };

function withOrWithout(ids: readonly string[], id: string, on: boolean): string[] {
  const rest = ids.filter((x) => x !== id);
  return on ? [...rest, id] : rest;
}

/**
 * Builds what the POS would receive for this item, so the preview renders the
 * real picker instead of a copy (spec §6). Unavailable sizes and options are
 * hidden, as on the POS.
 */
export function toSellablePreview(
  item: CatItem,
  model: CatalogModel,
  override?: PreviewOverride,
): CatalogSellableItemResponse {
  let inherited = model.categories.find((c) => c.id === item.categoryId)?.groupIds ?? [];
  let direct = item.directGroupIds;
  if (override) {
    inherited = withOrWithout(inherited, override.groupId, override.categoryIds.includes(item.categoryId));
    direct = withOrWithout(direct, override.groupId, override.itemIds.includes(item.id));
  }
  const groups = new Map(model.groups.map((g) => [g.id, g]));
  const modifierGroups = effectiveGroupIds(inherited, direct, item.excludedGroupIds).flatMap((id) => {
    const g = groups.get(id);
    if (!g) return [];
    return [
      {
        id: g.id,
        name: g.name,
        min_selections: g.min,
        max_selections: g.max,
        default_option_ids: g.defaultOptionIds,
        options: g.options.filter((o) => o.available).map((o) => ({ id: o.id, name: o.name, surcharge_vnd: o.surchargeVnd })),
      },
    ];
  });
  return {
    id: item.id,
    name: item.name,
    category_id: item.categoryId,
    code: item.code ?? undefined,
    image_url: item.imageUrl ?? undefined,
    price_vnd: item.priceVnd ?? undefined,
    sizes:
      item.priceVnd === null
        ? item.sizes.filter((s) => s.available).map((s) => ({ id: s.id, name: s.name, price_vnd: s.priceVnd }))
        : [],
    modifier_groups: modifierGroups,
  };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && bun test src/features/catalog/lib/sellable-preview.test.ts`
Expected: PASS, 3 tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/catalog/lib/sellable-preview.ts web/src/features/catalog/lib/sellable-preview.test.ts
git commit -m "feat(web): build the POS preview item for the batch linker"
```

---

### Task 8: Split `ItemPickerBody` out of `ItemPickerDialog`

**Files:**
- Modify: `web/src/features/pos/components/item-picker-dialog.tsx` (whole component, currently lines 54 to the end)
- Test: `web/src/features/pos/components/item-picker-dialog.test.tsx` (add one test)

**Interfaces:**
- Consumes: nothing new.
- Produces: `ItemPickerBodyProps { item: CatalogSellableItemResponse; initialValues?: Partial<ItemPickerConfig>; onConfirm: (config: ItemPickerConfig) => void; isSubmitting?: boolean; confirmLabel?: string }` and `ItemPickerBody(props)`. It renders a fragment: the scrollable content, then the footer. Callers remount it with `key` to reset its state. `ItemPickerDialog`'s props and behavior are unchanged.

- [ ] **Step 1: Write the failing test**

Append to `web/src/features/pos/components/item-picker-dialog.test.tsx`, and add `ItemPickerBody` to the existing import from `./item-picker-dialog`:

```tsx
describe("ItemPickerBody", () => {
  it("renders the picker controls without the dialog chrome", () => {
    const html = renderToString(
      <ItemPickerBody item={mockItemWithSizesAndModifiers} onConfirm={() => {}} confirmLabel="Xem trước" />,
    );
    expect(html).toContain("Nhỏ (S)");
    expect(html).toContain("Độ ngọt");
    expect(html).toContain("Xem trước");
    expect(html).not.toContain('role="dialog"');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/pos/components/item-picker-dialog.test.tsx`
Expected: FAIL, `ItemPickerBody` is not exported.

- [ ] **Step 3: Restructure the component**

In `item-picker-dialog.tsx`, replace everything from `export function ItemPickerDialog(` to the end of the file with the code below. The two blocks marked `MOVE` are the existing JSX, moved verbatim: the `<div className="flex-1 overflow-y-auto p-5 space-y-5">` block under `{/* Dialog Scrollable Content */}`, and the `<div className="flex items-center justify-between border-t ...">` block under `{/* Dialog Footer (Quantity Stepper & Confirm CTA) */}`. The `React.useEffect` that re-synced state on open and the `prevIsOpenRef`/`prevItemIdRef` refs are deleted. The dialog mounts the body only while it is open and keys it by item id, so the `useState` initializers now do that job.

```tsx
export interface ItemPickerBodyProps {
  item: CatalogSellableItemResponse;
  initialValues?: Partial<ItemPickerConfig>;
  onConfirm: (config: ItemPickerConfig) => void;
  isSubmitting?: boolean;
  confirmLabel?: string;
}

/**
 * Size, modifier, note, and quantity controls with the confirm footer. State
 * initializes on mount, so callers remount it with `key` to reset it. The
 * slice 9b Batch Linker renders it as a live preview.
 */
export function ItemPickerBody({
  item,
  initialValues,
  onConfirm,
  isSubmitting = false,
  confirmLabel = "Thêm vào đơn",
}: ItemPickerBodyProps) {
  const [initial] = React.useState(() => getInitialConfig(item, initialValues));
  const [sizeId, setSizeId] = React.useState<string | undefined>(initial.sizeId);
  const [selectedOptionIds, setSelectedOptionIds] = React.useState<string[]>(initial.selectedOptionIds);
  const [note, setNote] = React.useState<string>(initial.preparationNote);
  const [quantity, setQuantity] = React.useState<number>(initial.quantity);

  const hasSizes = Boolean(item.sizes && item.sizes.length > 0);
  const selectedSize = item.sizes?.find((s) => s.id === sizeId);
  const basePrice = selectedSize?.price_vnd ?? item.price_vnd ?? 0;

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
    <>
      {/* Dialog Scrollable Content */}
      {/* MOVE: the existing <div className="flex-1 overflow-y-auto p-5 space-y-5"> … </div> block, unchanged */}

      {/* Dialog Footer (Quantity Stepper & Confirm CTA) */}
      {/* MOVE: the existing <div className="flex items-center justify-between border-t border-border p-4 bg-muted/20 gap-4"> … </div> block, unchanged */}
    </>
  );
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
  if (!isOpen || !item) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-xs animate-in fade-in duration-150">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="picker-dialog-title"
        className="flex flex-col w-full max-w-lg max-h-[90vh] rounded-2xl border border-border bg-card shadow-2xl overflow-hidden animate-in fade-in zoom-in-95 duration-150"
      >
        {/* Dialog Header: the existing header block, unchanged */}
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
            className="h-12 w-12 min-h-[48px] min-w-[48px] rounded-xl text-muted-foreground hover:text-foreground hover:bg-muted flex items-center justify-center select-none active:scale-[0.98] transition"
            aria-label="Đóng"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        <ItemPickerBody
          key={item.id}
          item={item}
          initialValues={initialValues}
          onConfirm={onConfirm}
          isSubmitting={isSubmitting}
          confirmLabel={confirmLabel}
        />
      </div>
    </div>
  );
}
```

`getInitialConfig` stays as it is. Its `item === null` branch is now unused by callers but harmless.

- [ ] **Step 4: Run the POS tests**

Run: `cd web && bun test src/features/pos`
Expected: PASS. All existing tests pass unchanged, plus the new `ItemPickerBody` test.

- [ ] **Step 5: Lint and type-check**

Run: `cd web && bunx tsc -b && bun run lint`
Expected: exit 0 for both. The `oxlint-disable-next-line react/set-state-in-effect` comments are gone along with the effect.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/pos/components/item-picker-dialog.tsx web/src/features/pos/components/item-picker-dialog.test.tsx
git commit -m "refactor(web): split ItemPickerBody out of ItemPickerDialog"
```

---

### Task 9: Shared form UI, save progress, and retire dialog

**Files:**
- Create: `web/src/features/catalog/components/form-bits.tsx`
- Create: `web/src/features/catalog/components/save-progress.tsx`
- Create: `web/src/features/catalog/components/retire-dialog.tsx`
- Test: `web/src/features/catalog/components/save-progress.test.tsx`
- Test: `web/src/features/catalog/components/retire-dialog.test.tsx`

**Interfaces:**
- Consumes: `StepResult` (Task 5); `Retirement`, `RETIRE_REASONS`, `RETIRE_REASON_LABELS` (Task 1).
- Produces:
  - `form-bits.tsx`: `INPUT_CLASS`, `MONEY_INPUT_CLASS`, `readNumber(value)`, `ModalFrame(props)`, `Field(props)`, `Chip(props)`, `FormFooter(props)`.
  - `save-progress.tsx`: `SAVE_INCOMPLETE`, `SaveProgress({ results })`.
  - `retire-dialog.tsx`: `RETIRE_WARNING`, `NOTE_REQUIRED`, `RetirePanel(props)`, `RetireDialog({ open, name, onConfirm, onClose })`, where `onConfirm: (r: Retirement) => Promise<string | null> | string | null` returns an error message or null.

- [ ] **Step 1: Write the failing tests**

```tsx
// web/src/features/catalog/components/save-progress.test.tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { SAVE_INCOMPLETE, SaveProgress } from "./save-progress";

describe("SaveProgress", () => {
  it("renders nothing before a save or after a full success", () => {
    expect(renderToString(<SaveProgress results={null} />)).toBe("");
    expect(renderToString(<SaveProgress results={[{ label: "Đổi tên món", type: "item.rename", status: "done" }]} />)).toBe("");
  });

  it("lists each step with the failure message", () => {
    const html = renderToString(
      <SaveProgress
        results={[
          { label: "Đổi tên món", type: "item.rename", status: "done" },
          { label: "Đổi giá món", type: "item.reprice", status: "failed", error: "Mã PIN sai" },
          { label: "Cập nhật nhóm topping", type: "item.groups", status: "pending" },
        ]}
      />,
    );
    expect(html).toContain(SAVE_INCOMPLETE);
    expect(html).toContain("Đổi giá món");
    expect(html).toContain("Mã PIN sai");
    expect(html).toContain("Cập nhật nhóm topping");
  });
});
```

```tsx
// web/src/features/catalog/components/retire-dialog.test.tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { RETIRE_WARNING, RetirePanel } from "./retire-dialog";

const base = {
  name: "Trà đào",
  value: { reason: "NO_LONGER_OFFERED" as const, note: "" },
  error: null,
  busy: false,
  onChange: () => {},
  onConfirm: () => {},
  onClose: () => {},
};

describe("RetirePanel", () => {
  it("names the entity, warns, and offers the three reasons", () => {
    const html = renderToString(<RetirePanel {...base} />);
    expect(html).toContain("Trà đào");
    expect(html).toContain(RETIRE_WARNING);
    for (const label of ["Không bán nữa", "Sắp xếp lại thực đơn", "Khác"]) expect(html).toContain(label);
  });

  it("shows an inline error", () => {
    expect(renderToString(<RetirePanel {...base} error="Vui lòng ghi chú lý do" />)).toContain("Vui lòng ghi chú lý do");
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun test src/features/catalog/components`
Expected: FAIL, modules not found.

- [ ] **Step 3: Write `form-bits.tsx`**

```tsx
// web/src/features/catalog/components/form-bits.tsx
import type { ReactElement, ReactNode } from "react";
import { Trash2, X, type LucideIcon } from "lucide-react";
import { useHotkeys } from "react-hotkeys-hook";
import { cn } from "@/lib/utils";

export const INPUT_CLASS =
  "h-12 min-h-[48px] w-full rounded-xl border border-slate-300 bg-white px-4 text-xs font-semibold text-slate-900 outline-none transition focus:border-emerald-500 focus:ring-2 focus:ring-emerald-200 sm:text-sm dark:border-border dark:bg-card dark:text-foreground";

export const MONEY_INPUT_CLASS = cn(INPUT_CLASS, "font-mono text-base font-bold text-emerald-700 dark:text-emerald-400");

/** An empty number input reads as 0 so validation reports it. */
export function readNumber(value: string): number {
  const n = Number(value);
  return Number.isNaN(n) ? 0 : n;
}

export interface ModalFrameProps {
  titleId: string;
  icon: LucideIcon;
  title: string;
  subtitle: string;
  wide?: boolean;
  /** Disables Esc and Ctrl+S while saving or while a child dialog is open. */
  busy: boolean;
  onClose: () => void;
  onSave: () => void;
  footer: ReactNode;
  children: ReactNode;
}

/** The prototype's modal chrome (settings.html #modal-item-form). */
export function ModalFrame({
  titleId,
  icon: Icon,
  title,
  subtitle,
  wide = false,
  busy,
  onClose,
  onSave,
  footer,
  children,
}: ModalFrameProps): ReactElement {
  useHotkeys("esc", onClose, { enabled: !busy, enableOnFormTags: true });
  useHotkeys(
    "mod+s",
    (event) => {
      event.preventDefault();
      onSave();
    },
    { enabled: !busy, enableOnFormTags: true },
  );
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-slate-900/40 p-3 backdrop-blur-xs sm:p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={cn(
          "flex max-h-[92vh] w-full flex-col overflow-hidden rounded-2xl border border-slate-200 bg-card shadow-modal animate-in fade-in zoom-in-95 dark:border-border",
          wide ? "max-w-4xl" : "max-w-xl",
        )}
      >
        <div className="flex items-start justify-between gap-3 border-b border-slate-100 p-4 sm:p-5 dark:border-border">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-emerald-200 bg-emerald-50 text-emerald-600 shadow-2xs dark:border-emerald-900 dark:bg-emerald-950/40">
              <Icon className="h-5 w-5" />
            </div>
            <div>
              <h3 id={titleId} className="text-base leading-tight font-bold text-slate-900 sm:text-lg dark:text-foreground">
                {title}
              </h3>
              <p className="mt-0.5 text-xs text-slate-500 dark:text-muted-foreground">{subtitle}</p>
            </div>
          </div>
          <button
            type="button"
            aria-label="Đóng"
            onClick={onClose}
            className="flex h-12 min-h-[48px] w-12 min-w-[48px] shrink-0 items-center justify-center rounded-xl text-slate-400 transition hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-muted"
          >
            <X className="h-5 w-5" />
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-4 sm:p-5">{children}</div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-slate-100 bg-slate-50/60 p-4 dark:border-border dark:bg-muted/20">
          {footer}
        </div>
      </div>
    </div>
  );
}

export interface FieldProps {
  label: string;
  htmlFor?: string;
  required?: boolean;
  error?: string;
  hint?: string;
  children: ReactNode;
}

export function Field({ label, htmlFor, required, error, hint, children }: FieldProps): ReactElement {
  return (
    <div className="space-y-1.5">
      <label htmlFor={htmlFor} className="text-xs font-bold text-slate-700 dark:text-foreground">
        {label}
        {required && <span className="ml-0.5 text-rose-600">*</span>}
      </label>
      {children}
      {error ? (
        <p role="alert" className="text-[11px] font-semibold text-rose-600">
          {error}
        </p>
      ) : hint ? (
        <p className="text-[11px] text-slate-400">{hint}</p>
      ) : null}
    </div>
  );
}

export interface ChipProps {
  active: boolean;
  disabled?: boolean;
  title?: string;
  onClick: () => void;
  children: ReactNode;
}

export function Chip({ active, disabled, title, onClick, children }: ChipProps): ReactElement {
  return (
    <button
      type="button"
      aria-pressed={active}
      disabled={disabled}
      title={title}
      onClick={onClick}
      className={cn(
        "flex h-12 min-h-[48px] items-center gap-2 rounded-xl border px-3.5 text-xs font-semibold transition select-none active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50",
        active
          ? "border-emerald-600 bg-emerald-50 text-emerald-900 ring-2 ring-emerald-200 dark:bg-emerald-950/40 dark:text-emerald-300"
          : "border-slate-200 bg-white text-slate-700 hover:bg-slate-50 dark:border-border dark:bg-card dark:text-foreground dark:hover:bg-muted",
      )}
    >
      {children}
    </button>
  );
}

export interface FormFooterProps {
  retireLabel: string;
  onRetire?: () => void;
  onClose: () => void;
  onSave: () => void;
  saveLabel: string;
  busy: boolean;
}

export function FormFooter({ retireLabel, onRetire, onClose, onSave, saveLabel, busy }: FormFooterProps): ReactElement {
  return (
    <>
      <div>
        {onRetire && (
          <button
            type="button"
            onClick={onRetire}
            disabled={busy}
            className="flex h-12 min-h-[48px] items-center gap-2 rounded-xl border border-rose-200 bg-rose-50 px-4 text-xs font-bold text-rose-700 transition hover:bg-rose-100 active:scale-[0.98] sm:text-sm dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-400"
          >
            <Trash2 className="h-4 w-4" />
            {retireLabel}
          </button>
        )}
      </div>
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={onClose}
          disabled={busy}
          className="h-12 min-h-[48px] rounded-xl border border-slate-200 bg-white px-5 text-xs font-bold text-slate-700 transition hover:bg-slate-100 active:scale-[0.98] sm:text-sm dark:border-border dark:bg-card dark:text-foreground"
        >
          Hủy bỏ
        </button>
        <button
          type="button"
          onClick={onSave}
          disabled={busy}
          className="flex h-12 min-h-[48px] items-center gap-2 rounded-xl bg-emerald-600 px-6 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 active:scale-[0.98] disabled:opacity-60 sm:text-sm"
        >
          {busy ? "Đang lưu…" : saveLabel}
        </button>
      </div>
    </>
  );
}
```

- [ ] **Step 4: Write `save-progress.tsx`**

```tsx
// web/src/features/catalog/components/save-progress.tsx
import type { ReactElement } from "react";
import { Check, Circle, X } from "lucide-react";
import type { StepResult } from "../lib/run-plan";

export const SAVE_INCOMPLETE = "Lưu chưa hoàn tất. Sửa lỗi rồi bấm Lưu để tiếp tục phần còn lại.";

/** Shown after a save stopped part-way (spec §4.4). */
export function SaveProgress({ results }: { results: StepResult[] | null }): ReactElement | null {
  if (!results || results.every((r) => r.status === "done")) return null;
  return (
    <div role="alert" className="space-y-2 rounded-xl border border-rose-200 bg-rose-50/60 p-3 dark:border-rose-900 dark:bg-rose-950/20">
      <p className="text-xs font-bold text-rose-700 dark:text-rose-400">{SAVE_INCOMPLETE}</p>
      <ul className="space-y-1 text-xs">
        {results.map((r, i) => (
          <li key={i} className="flex items-start gap-2">
            {r.status === "done" ? (
              <Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-600" />
            ) : r.status === "failed" ? (
              <X className="mt-0.5 h-3.5 w-3.5 shrink-0 text-rose-600" />
            ) : (
              <Circle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-slate-300" />
            )}
            <span className={r.status === "pending" ? "text-slate-400" : "text-slate-800 dark:text-foreground"}>
              {r.label}
              {r.error && <span className="font-semibold text-rose-700 dark:text-rose-400"> — {r.error}</span>}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
```

- [ ] **Step 5: Write `retire-dialog.tsx`**

```tsx
// web/src/features/catalog/components/retire-dialog.tsx
import { useState, type ReactElement } from "react";
import { Trash2 } from "lucide-react";
import { RETIRE_REASON_LABELS, RETIRE_REASONS, type Retirement } from "../lib/catalog-model";
import { Chip } from "./form-bits";

export const RETIRE_WARNING = "Không thể hoàn tác. Lịch sử bán hàng vẫn được giữ.";
export const NOTE_REQUIRED = "Vui lòng ghi chú lý do";

const INITIAL: Retirement = { reason: "NO_LONGER_OFFERED", note: "" };

export interface RetirePanelProps {
  name: string;
  value: Retirement;
  error: string | null;
  busy: boolean;
  onChange: (value: Retirement) => void;
  onConfirm: () => void;
  onClose: () => void;
}

export function RetirePanel({ name, value, error, busy, onChange, onConfirm, onClose }: RetirePanelProps): ReactElement {
  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-slate-900/40 p-4 backdrop-blur-xs">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="retire-title"
        className="flex w-full max-w-md flex-col gap-4 rounded-2xl border border-slate-200 bg-card p-5 shadow-modal animate-in fade-in zoom-in-95 dark:border-border"
      >
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-rose-200 bg-rose-50 text-rose-600 dark:border-rose-900 dark:bg-rose-950/40">
            <Trash2 className="h-5 w-5" />
          </div>
          <div>
            <h2 id="retire-title" className="text-base font-bold text-slate-900 dark:text-foreground">
              Ngừng bán “{name}”
            </h2>
            <p className="text-xs text-slate-500 dark:text-muted-foreground">{RETIRE_WARNING}</p>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          {RETIRE_REASONS.map((reason) => (
            <Chip key={reason} active={value.reason === reason} onClick={() => onChange({ ...value, reason })}>
              {RETIRE_REASON_LABELS[reason]}
            </Chip>
          ))}
        </div>
        <textarea
          aria-label="Ghi chú"
          rows={2}
          value={value.note}
          placeholder="Ghi chú (bắt buộc khi chọn Khác)"
          onChange={(e) => onChange({ ...value, note: e.target.value })}
          className="min-h-[72px] w-full resize-none rounded-xl border border-slate-300 p-3 text-xs text-slate-700 outline-none transition focus:border-emerald-500 focus:ring-2 focus:ring-emerald-200 dark:border-border dark:bg-card dark:text-foreground"
        />
        {error && (
          <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 px-3 py-2 text-xs font-semibold text-rose-700 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-400">
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="h-12 min-h-[48px] rounded-xl border border-slate-200 bg-white px-5 text-xs font-bold text-slate-700 hover:bg-slate-100 sm:text-sm dark:border-border dark:bg-card dark:text-foreground"
          >
            Hủy bỏ
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={busy}
            className="h-12 min-h-[48px] rounded-xl bg-rose-600 px-6 text-xs font-bold text-white hover:bg-rose-700 disabled:opacity-60 sm:text-sm"
          >
            {busy ? "Đang xử lý…" : "Ngừng bán"}
          </button>
        </div>
      </div>
    </div>
  );
}

export interface RetireDialogProps {
  open: boolean;
  name: string;
  /** Resolves to an error message to show, or null when done. */
  onConfirm: (retirement: Retirement) => Promise<string | null> | string | null;
  onClose: () => void;
}

export function RetireDialog({ open, name, onConfirm, onClose }: RetireDialogProps): ReactElement | null {
  const [value, setValue] = useState<Retirement>(INITIAL);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  if (!open) return null;

  const reset = () => {
    setValue(INITIAL);
    setError(null);
  };
  const confirm = async () => {
    if (value.reason === "OTHER" && !value.note.trim()) {
      setError(NOTE_REQUIRED);
      return;
    }
    setBusy(true);
    const failure = await onConfirm(value);
    setBusy(false);
    if (failure) setError(failure);
    else reset();
  };

  return (
    <RetirePanel
      name={name}
      value={value}
      error={error}
      busy={busy}
      onChange={(v) => {
        setValue(v);
        setError(null);
      }}
      onConfirm={() => void confirm()}
      onClose={() => {
        reset();
        onClose();
      }}
    />
  );
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd web && bun test src/features/catalog/components`
Expected: PASS, 4 tests.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/catalog/components/form-bits.tsx web/src/features/catalog/components/save-progress.tsx web/src/features/catalog/components/save-progress.test.tsx web/src/features/catalog/components/retire-dialog.tsx web/src/features/catalog/components/retire-dialog.test.tsx
git commit -m "feat(web): catalog modal chrome, save progress, and retire dialog"
```

---

### Task 10: Item modal

**Files:**
- Create: `web/src/features/catalog/components/item-form-panel.tsx`
- Create: `web/src/features/catalog/components/item-form-modal.tsx`
- Test: `web/src/features/catalog/components/item-form-panel.test.tsx`

**Interfaces:**
- Consumes: Tasks 1 to 6 and 9. `messageForError` from `@/lib/error-messages`; `playErrorBuzz` from `@/lib/sound`; `getAcronym` from `@/lib/search`.
- Produces:
  - `ItemFormPanelProps` and `ItemFormPanel(props)`, presentational.
  - `ItemFormModalProps { snapshot: CatItem | null; model: CatalogModel; defaultCategoryId: string; onClose: () => void; onCreated: (itemId: string) => void; onSaved: (message: string) => void }` and `ItemFormModal(props)`. The parent mounts one instance per opening, keyed, and keeps `snapshot` current from the refetched model.

- [ ] **Step 1: Write the failing test**

```tsx
// web/src/features/catalog/components/item-form-panel.test.tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import type { CatCategory, CatGroup } from "../lib/catalog-model";
import { itemFormFrom } from "../lib/forms";
import { ItemFormPanel, type ItemFormPanelProps } from "./item-form-panel";

const categories: CatCategory[] = [{ id: "c-tea", name: "Trà", icon: null, displayOrder: 1, groupIds: ["g-ice"] }];
const groups: CatGroup[] = [
  { id: "g-ice", name: "Mức đá", min: 1, max: 1, options: [], defaultOptionIds: [] },
  { id: "g-top", name: "Topping trà", min: 0, max: 3, options: [], defaultOptionIds: [] },
];

function props(patch: Partial<ItemFormPanelProps>): ItemFormPanelProps {
  return {
    form: itemFormFrom(null, "c-tea"),
    errors: {},
    isEdit: false,
    busy: false,
    results: null,
    categories,
    groups,
    storedImageUrl: null,
    onChange: () => {},
    onPickImage: () => {},
    onRemoveSize: () => {},
    onSave: () => {},
    onClose: () => {},
    ...patch,
  };
}

describe("ItemFormPanel", () => {
  it("offers the pricing mode toggle only when creating", () => {
    expect(renderToString(<ItemFormPanel {...props({})} />)).toContain("Định giá Đa Kích cỡ");
    expect(renderToString(<ItemFormPanel {...props({ isEdit: true })} />)).not.toContain("Định giá Đa Kích cỡ");
  });

  it("lists inherited groups apart from the extra group chips", () => {
    const html = renderToString(<ItemFormPanel {...props({})} />);
    expect(html).toContain("Nhóm tùy chọn Kế thừa từ Danh mục");
    expect(html).toContain("Mức đá");
    expect(html).toContain("Topping trà");
  });

  it("shows field errors and the retire button when editing", () => {
    const html = renderToString(
      <ItemFormPanel {...props({ isEdit: true, errors: { name: "Vui lòng nhập tên món" }, onRetire: () => {} })} />,
    );
    expect(html).toContain("Vui lòng nhập tên món");
    expect(html).toContain("Xóa món ăn");
    expect(html).toContain("LƯU MÓN ĂN (Ctrl+S)");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/catalog/components/item-form-panel.test.tsx`
Expected: FAIL, module not found.

- [ ] **Step 3: Write `item-form-panel.tsx`**

```tsx
// web/src/features/catalog/components/item-form-panel.tsx
import type { ReactElement } from "react";
import { Coffee, ImagePlus, Plus, Trash2, Utensils } from "lucide-react";
import { getAcronym } from "@/lib/search";
import { cn } from "@/lib/utils";
import { BADGE_LABELS, BADGES, type CatCategory, type CatGroup } from "../lib/catalog-model";
import { blankSizeRow, type ItemForm, type SizeRow } from "../lib/forms";
import { changeCategory, inheritedGroupIds, toggleDirect, toggleExcluded } from "../lib/item-inheritance";
import type { StepResult } from "../lib/run-plan";
import { MAX_DESCRIPTION, type FieldErrors } from "../lib/validation";
import { Chip, Field, FormFooter, INPUT_CLASS, ModalFrame, MONEY_INPUT_CLASS, readNumber } from "./form-bits";
import { SaveProgress } from "./save-progress";

export interface ItemFormPanelProps {
  form: ItemForm;
  errors: FieldErrors;
  isEdit: boolean;
  busy: boolean;
  results: StepResult[] | null;
  categories: CatCategory[];
  groups: CatGroup[];
  storedImageUrl: string | null;
  onChange: (next: ItemForm) => void;
  onPickImage: (file: File) => void;
  onRemoveSize: (key: string) => void;
  onSave: () => void;
  onClose: () => void;
  onRetire?: () => void;
}

const ERROR_TEXT = "text-[11px] font-semibold text-rose-600";

function groupSummary(g: CatGroup): string {
  return `${g.options.length} lựa chọn · ${g.max === 1 ? "chọn 1" : `tối đa ${g.max}`}`;
}

export function ItemFormPanel(p: ItemFormPanelProps): ReactElement {
  const { form, errors, onChange } = p;
  const inherited = inheritedGroupIds(p.categories, form.categoryId);
  const byId = new Map(p.groups.map((g) => [g.id, g]));
  const extra = p.groups.filter((g) => !inherited.includes(g.id));
  const imageUrl =
    form.image.kind === "upload" ? form.image.previewUrl : form.image.kind === "clear" ? null : p.storedImageUrl;
  const setSize = (key: string, patch: Partial<SizeRow>) =>
    onChange({ ...form, sizes: form.sizes.map((s) => (s.key === key ? { ...s, ...patch } : s)) });
  const toggleMode = () =>
    form.mode === "sizes"
      ? onChange({ ...form, mode: "single" })
      : onChange({ ...form, mode: "sizes", sizes: form.sizes.length > 0 ? form.sizes : [blankSizeRow()] });

  return (
    <ModalFrame
      titleId="item-form-title"
      icon={Utensils}
      wide
      title={p.isEdit ? "Chỉnh sửa món" : "Thêm món mới vào thực đơn"}
      subtitle="Thiết lập cấu hình món, ma trận kích cỡ giá bán và liên kết nhóm tùy chọn topping."
      busy={p.busy}
      onClose={p.onClose}
      onSave={p.onSave}
      footer={
        <FormFooter
          retireLabel="Xóa món ăn"
          onRetire={p.onRetire}
          onClose={p.onClose}
          onSave={p.onSave}
          saveLabel="LƯU MÓN ĂN (Ctrl+S)"
          busy={p.busy}
        />
      }
    >
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <div className="space-y-4">
          <Field label="Tên món ăn / đồ uống" htmlFor="item-name" required error={errors.name}>
            <input
              id="item-name"
              className={INPUT_CLASS}
              value={form.name}
              placeholder="Ví dụ: Trà đào cam sả"
              onChange={(e) => onChange({ ...form, name: e.target.value })}
            />
          </Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Mã viết tắt (Acronym)" htmlFor="item-code" error={errors.code} hint="Để trống sẽ dùng mã tự tạo">
              <input
                id="item-code"
                className={cn(INPUT_CLASS, "font-mono text-emerald-700 uppercase")}
                value={form.code}
                placeholder={getAcronym(form.name) || "tdcs"}
                onChange={(e) => onChange({ ...form, code: e.target.value })}
              />
            </Field>
            <Field label="Danh mục thực đơn" htmlFor="item-category" error={errors.categoryId}>
              <select
                id="item-category"
                className={INPUT_CLASS}
                value={form.categoryId}
                onChange={(e) => onChange(changeCategory(form, p.categories, e.target.value))}
              >
                {p.categories.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            </Field>
          </div>

          {!p.isEdit && (
            <div className="flex items-center justify-between gap-3 rounded-xl border border-slate-200 bg-slate-50 p-3 dark:border-border dark:bg-muted/30">
              <div>
                <p className="text-xs font-bold text-slate-800 dark:text-foreground">Định giá Đa Kích cỡ (S / M / L)</p>
                <p className="text-[11px] text-slate-500">Bật nếu món có nhiều kích cỡ khác nhau với các mức giá riêng biệt.</p>
              </div>
              <Chip active={form.mode === "sizes"} onClick={toggleMode}>
                {form.mode === "sizes" ? "Đang BẬT" : "Đang TẮT"}
              </Chip>
            </div>
          )}

          {form.mode === "single" ? (
            <Field label="Đơn giá bán (VND)" htmlFor="item-price" error={errors.priceVnd}>
              <input
                id="item-price"
                type="number"
                min={1}
                step={1000}
                className={MONEY_INPUT_CLASS}
                value={form.priceVnd || ""}
                placeholder="35000"
                onChange={(e) => onChange({ ...form, priceVnd: readNumber(e.target.value) })}
              />
            </Field>
          ) : (
            <div className="space-y-2">
              <p className="text-xs font-bold text-slate-700 dark:text-foreground">Danh sách Kích cỡ & Giá bán</p>
              {form.sizes.map((s) => (
                <div key={s.key} className="space-y-1">
                  <div className="flex items-center gap-2">
                    <input
                      aria-label="Tên kích cỡ"
                      className={INPUT_CLASS}
                      value={s.name}
                      placeholder="Size M"
                      onChange={(e) => setSize(s.key, { name: e.target.value })}
                    />
                    <input
                      aria-label="Giá bán"
                      type="number"
                      min={1}
                      step={1000}
                      className={MONEY_INPUT_CLASS}
                      value={s.priceVnd || ""}
                      placeholder="35000"
                      onChange={(e) => setSize(s.key, { priceVnd: readNumber(e.target.value) })}
                    />
                    <button
                      type="button"
                      aria-label="Xóa kích cỡ này"
                      disabled={form.sizes.length <= 1}
                      onClick={() => p.onRemoveSize(s.key)}
                      className="flex h-12 min-h-[48px] w-12 min-w-[48px] shrink-0 items-center justify-center rounded-lg border border-slate-200 text-slate-400 transition hover:bg-rose-50 hover:text-rose-600 disabled:pointer-events-none disabled:opacity-40 dark:border-border"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                  {errors[`size.${s.key}`] && (
                    <p role="alert" className={ERROR_TEXT}>
                      {errors[`size.${s.key}`]}
                    </p>
                  )}
                </div>
              ))}
              {errors.sizes && (
                <p role="alert" className={ERROR_TEXT}>
                  {errors.sizes}
                </p>
              )}
              <button
                type="button"
                onClick={() => onChange({ ...form, sizes: [...form.sizes, blankSizeRow()] })}
                className="flex h-12 min-h-[48px] items-center gap-1.5 rounded-xl border border-dashed border-emerald-300 bg-white px-3.5 text-xs font-bold text-emerald-700 transition hover:bg-emerald-50 active:scale-[0.98] dark:bg-card"
              >
                <Plus className="h-4 w-4" />
                Thêm kích cỡ
              </button>
            </div>
          )}

          <Field label="Ảnh món" htmlFor="item-image" error={errors.image} hint="JPEG, PNG hoặc WebP. Ảnh được thu nhỏ trước khi tải lên.">
            <div className="flex items-center gap-3">
              <div className="flex h-20 w-20 shrink-0 items-center justify-center overflow-hidden rounded-xl border border-slate-200 bg-slate-100 dark:border-border dark:bg-muted">
                {imageUrl ? <img src={imageUrl} alt="" className="h-full w-full object-cover" /> : <Coffee className="h-7 w-7 text-slate-400" />}
              </div>
              <label className="flex h-12 min-h-[48px] cursor-pointer items-center gap-2 rounded-xl border border-slate-200 bg-white px-3.5 text-xs font-bold text-slate-700 transition hover:bg-slate-50 dark:border-border dark:bg-card dark:text-foreground">
                <ImagePlus className="h-4 w-4" />
                Chọn ảnh
                <input
                  id="item-image"
                  type="file"
                  accept="image/*"
                  className="sr-only"
                  onChange={(e) => {
                    const file = e.target.files?.[0];
                    if (file) p.onPickImage(file);
                    e.target.value = "";
                  }}
                />
              </label>
              {imageUrl && (
                <button
                  type="button"
                  onClick={() => onChange({ ...form, image: { kind: "clear" } })}
                  className="h-12 min-h-[48px] rounded-xl px-3 text-xs font-semibold text-rose-600 hover:bg-rose-50"
                >
                  Gỡ ảnh
                </button>
              )}
            </div>
          </Field>

          <div className="space-y-1.5">
            <p className="text-xs font-bold text-slate-700 dark:text-foreground">Huy hiệu Tiếp thị (Marketing Badge)</p>
            <div className="flex flex-wrap gap-2">
              <Chip active={form.badge === null} onClick={() => onChange({ ...form, badge: null })}>
                Không có
              </Chip>
              {BADGES.map((b) => (
                <Chip key={b} active={form.badge === b} onClick={() => onChange({ ...form, badge: b })}>
                  {BADGE_LABELS[b]}
                </Chip>
              ))}
            </div>
          </div>

          <Field
            label="Mô tả món ăn"
            htmlFor="item-description"
            error={errors.description}
            hint={`${Array.from(form.description).length}/${MAX_DESCRIPTION}`}
          >
            <textarea
              id="item-description"
              rows={2}
              value={form.description}
              placeholder="Ghi chú thành phần, hương vị đặc trưng..."
              onChange={(e) => onChange({ ...form, description: e.target.value })}
              className="min-h-[72px] w-full resize-none rounded-xl border border-slate-300 p-3 text-xs text-slate-700 outline-none transition focus:border-emerald-500 focus:ring-2 focus:ring-emerald-200 dark:border-border dark:bg-card dark:text-foreground"
            />
          </Field>
        </div>

        <div className="space-y-5">
          <section className="space-y-2">
            <p className="text-xs font-bold text-slate-700 dark:text-foreground">Nhóm tùy chọn Kế thừa từ Danh mục</p>
            {inherited.length === 0 ? (
              <p className="text-[11px] text-slate-400">Danh mục này chưa có nhóm mặc định.</p>
            ) : (
              inherited.map((id) => {
                const g = byId.get(id);
                if (!g) return null;
                const excluded = form.excludedGroupIds.includes(id);
                return (
                  <div key={id} className="flex items-center justify-between gap-3 rounded-xl border border-slate-200 p-3 dark:border-border">
                    <div>
                      <p className={cn("text-xs font-bold", excluded ? "text-slate-400 line-through" : "text-slate-800 dark:text-foreground")}>
                        {g.name}
                      </p>
                      <p className="text-[11px] text-slate-400">{groupSummary(g)}</p>
                    </div>
                    <Chip active={!excluded} onClick={() => onChange(toggleExcluded(form, id))}>
                      {excluded ? "Đã loại trừ" : "Đang áp dụng"}
                    </Chip>
                  </div>
                );
              })
            )}
          </section>
          <section className="space-y-2">
            <div className="flex items-center justify-between">
              <p className="text-xs font-bold text-slate-700 dark:text-foreground">Nhóm Topping & Tùy chọn Bổ sung</p>
              <span className="text-[11px] text-slate-400">1 chạm để bật/tắt</span>
            </div>
            <div className="flex flex-wrap gap-2">
              {extra.map((g) => (
                <Chip key={g.id} active={form.directGroupIds.includes(g.id)} onClick={() => onChange(toggleDirect(form, g.id))}>
                  {g.name}
                  <span className="font-mono text-[10px] text-slate-400">{g.options.length}</span>
                </Chip>
              ))}
            </div>
          </section>
          <SaveProgress results={p.results} />
        </div>
      </div>
    </ModalFrame>
  );
}
```

- [ ] **Step 4: Write `item-form-modal.tsx`**

```tsx
// web/src/features/catalog/components/item-form-modal.tsx
import { useState, type ReactElement } from "react";
import { messageForError } from "@/lib/error-messages";
import { playErrorBuzz } from "@/lib/sound";
import { useCatalogSave, useRetireEntity } from "../api/use-catalog-admin";
import type { CatalogModel, CatItem } from "../lib/catalog-model";
import { itemFormFrom, removeSizeRow } from "../lib/forms";
import { resizeToWebp } from "../lib/image-resize";
import { planItemSave } from "../lib/save-plan";
import { hasErrors, validateItemForm, type FieldErrors } from "../lib/validation";
import { ItemFormPanel } from "./item-form-panel";
import { RetireDialog } from "./retire-dialog";

export interface ItemFormModalProps {
  /** Null while creating; the parent keeps it current from the refetched model. */
  snapshot: CatItem | null;
  model: CatalogModel;
  defaultCategoryId: string;
  onClose: () => void;
  /** A create step succeeded but a later step failed: continue in edit mode. */
  onCreated: (itemId: string) => void;
  onSaved: (message: string) => void;
}

type Retiring = { kind: "item" } | { kind: "size"; key: string; name: string } | null;

export function ItemFormModal({ snapshot, model, defaultCategoryId, onClose, onCreated, onSaved }: ItemFormModalProps): ReactElement {
  const [form, setForm] = useState(() => itemFormFrom(snapshot, defaultCategoryId));
  const [errors, setErrors] = useState<FieldErrors>({});
  const [retiring, setRetiring] = useState<Retiring>(null);
  const { save, isSaving, results } = useCatalogSave();
  const retireEntity = useRetireEntity();

  const handleSave = async () => {
    if (isSaving) return;
    const found = validateItemForm(form);
    setErrors(found);
    if (hasErrors(found)) {
      playErrorBuzz();
      return;
    }
    const outcome = await save(planItemSave(snapshot, form));
    if (outcome.cancelled) return;
    if (outcome.ok) {
      onSaved(`Đã lưu món ${form.name.trim()}`);
      onClose();
      return;
    }
    if (!snapshot && outcome.created.itemId) onCreated(outcome.created.itemId);
    // A blob cannot be diffed against a stored image, so a finished upload is
    // dropped from the form rather than sent again (spec §4.4).
    if (outcome.results.some((r) => r.type === "item.image.set" && r.status === "done")) {
      setForm((f) => ({ ...f, image: { kind: "keep" } }));
    }
  };

  const pickImage = (file: File) => {
    resizeToWebp(file)
      .then((blob) => {
        setErrors((e) => {
          const next = { ...e };
          delete next.image;
          return next;
        });
        setForm((f) => ({ ...f, image: { kind: "upload", blob, previewUrl: URL.createObjectURL(blob) } }));
      })
      .catch((err: unknown) => {
        playErrorBuzz();
        setErrors((e) => ({ ...e, image: messageForError(err) }));
      });
  };

  const removeSize = (key: string) => {
    const row = form.sizes.find((s) => s.key === key);
    if (!row) return;
    if (row.id) setRetiring({ kind: "size", key, name: row.name });
    else setForm((f) => removeSizeRow(f, key, null));
  };

  return (
    <>
      <ItemFormPanel
        form={form}
        errors={errors}
        isEdit={snapshot !== null}
        busy={isSaving || retiring !== null}
        results={results}
        categories={model.categories}
        groups={model.groups}
        storedImageUrl={snapshot?.imageUrl ?? null}
        onChange={setForm}
        onPickImage={pickImage}
        onRemoveSize={removeSize}
        onSave={() => void handleSave()}
        onClose={onClose}
        onRetire={snapshot ? () => setRetiring({ kind: "item" }) : undefined}
      />
      <RetireDialog
        open={retiring !== null}
        name={retiring?.kind === "size" ? retiring.name : form.name}
        onClose={() => setRetiring(null)}
        onConfirm={async (retirement) => {
          if (retiring?.kind === "size") {
            setForm((f) => removeSizeRow(f, retiring.key, retirement));
            setRetiring(null);
            return null;
          }
          if (!snapshot) return null;
          const failure = await retireEntity("item", snapshot.id, snapshot.name, retirement);
          if (failure) return failure;
          setRetiring(null);
          onSaved(`Đã ngừng bán ${snapshot.name}`);
          onClose();
          return null;
        }}
      />
    </>
  );
}
```

- [ ] **Step 5: Run the test and type-check**

Run: `cd web && bun test src/features/catalog/components/item-form-panel.test.tsx && bunx tsc -b`
Expected: PASS, 3 tests; tsc exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/catalog/components/item-form-panel.tsx web/src/features/catalog/components/item-form-panel.test.tsx web/src/features/catalog/components/item-form-modal.tsx
git commit -m "feat(web): menu item form modal with planned save"
```

---

### Task 11: Items view

**Files:**
- Create: `web/src/features/catalog/components/item-card.tsx`
- Create: `web/src/features/catalog/components/items-view.tsx`
- Test: `web/src/features/catalog/components/item-card.test.tsx`

**Interfaces:**
- Consumes: `CatItem`, `CatalogModel`, `BADGE_LABELS`, `displayCode`, `priceLabel`, `filterItems` (Task 1); `effectiveGroupIds`, `inheritedGroupIds` (Task 3).
- Produces: `ItemCard({ item, categoryName, groupCount, onOpen })`; `ItemsView({ model, onOpenItem })`, where `onOpenItem(id: string | null)` opens the item modal (null creates).

- [ ] **Step 1: Write the failing test**

```tsx
// web/src/features/catalog/components/item-card.test.tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { formatVND } from "@/lib/utils";
import type { CatItem } from "../lib/catalog-model";
import { ItemCard } from "./item-card";

const item: CatItem = {
  id: "i",
  categoryId: "c",
  name: "Cà phê sữa đá",
  code: null,
  badge: "BEST_SELLER",
  description: null,
  imageUrl: "/media/catalog/a.webp",
  priceVnd: 29000,
  sizes: [],
  directGroupIds: [],
  excludedGroupIds: [],
};

describe("ItemCard", () => {
  it("shows the image, derived code, badge, price, and group count", () => {
    const html = renderToString(<ItemCard item={item} categoryName="Cà phê" groupCount={2} onOpen={() => {}} />);
    expect(html).toContain("/media/catalog/a.webp");
    expect(html).toContain("cfsd");
    expect(html).toContain("Bán chạy");
    expect(html).toContain(formatVND(29000));
    expect(html).toContain("2 nhóm topping");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/catalog/components/item-card.test.tsx`
Expected: FAIL, module not found.

- [ ] **Step 3: Write `item-card.tsx`**

```tsx
// web/src/features/catalog/components/item-card.tsx
import type { ReactElement } from "react";
import { Coffee, Pencil } from "lucide-react";
import { BADGE_LABELS, displayCode, priceLabel, type CatItem } from "../lib/catalog-model";

export interface ItemCardProps {
  item: CatItem;
  categoryName: string;
  groupCount: number;
  onOpen: () => void;
}

export function ItemCard({ item, categoryName, groupCount, onOpen }: ItemCardProps): ReactElement {
  const facts = [categoryName, item.sizes.length > 0 ? `${item.sizes.length} kích cỡ` : null, `${groupCount} nhóm topping`]
    .filter(Boolean)
    .join(" · ");
  return (
    <button
      type="button"
      onClick={onOpen}
      className="group flex w-full gap-3.5 rounded-2xl border border-slate-200 bg-card p-3.5 text-left shadow-2xs transition hover:border-emerald-300 hover:shadow-sm active:scale-[0.99] dark:border-border"
    >
      <div className="flex h-20 w-20 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-slate-100 dark:bg-muted">
        {item.imageUrl ? (
          <img src={item.imageUrl} alt="" loading="lazy" className="h-full w-full object-cover" />
        ) : (
          <Coffee className="h-7 w-7 text-slate-400" />
        )}
      </div>
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex items-center gap-2">
          <span className="rounded-md bg-slate-100 px-1.5 py-0.5 font-mono text-[10px] font-bold text-slate-600 uppercase dark:bg-muted dark:text-muted-foreground">
            {displayCode(item)}
          </span>
          {item.badge && (
            <span className="rounded-md bg-amber-100 px-1.5 py-0.5 text-[10px] font-bold text-amber-800 dark:bg-amber-950/40 dark:text-amber-300">
              {BADGE_LABELS[item.badge]}
            </span>
          )}
        </div>
        <p className="truncate text-sm font-bold text-slate-900 dark:text-foreground">{item.name}</p>
        <p className="truncate text-[11px] text-slate-500 dark:text-muted-foreground">{facts}</p>
        <p className="font-mono text-sm font-bold text-emerald-700 dark:text-emerald-400">{priceLabel(item)}</p>
      </div>
      <Pencil className="h-4 w-4 shrink-0 text-slate-300 group-hover:text-emerald-600" />
    </button>
  );
}
```

- [ ] **Step 4: Write `items-view.tsx`**

```tsx
// web/src/features/catalog/components/items-view.tsx
import { useState, type ReactElement } from "react";
import { Plus, Search, X } from "lucide-react";
import { filterItems, type CatalogModel } from "../lib/catalog-model";
import { effectiveGroupIds, inheritedGroupIds } from "../lib/item-inheritance";
import { INPUT_CLASS } from "./form-bits";
import { ItemCard } from "./item-card";

export interface ItemsViewProps {
  model: CatalogModel;
  /** Null creates a new item. */
  onOpenItem: (itemId: string | null) => void;
}

export function ItemsView({ model, onOpenItem }: ItemsViewProps): ReactElement {
  const [query, setQuery] = useState("");
  const [categoryId, setCategoryId] = useState("all");
  const visible = filterItems(model, query, categoryId);
  const categoryName = new Map(model.categories.map((c) => [c.id, c.name]));
  const canAdd = model.categories.length > 0;

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-3 rounded-2xl border border-slate-200 bg-card p-4 shadow-2xs lg:flex-row lg:items-center dark:border-border">
        <div className="flex flex-1 flex-col gap-3 sm:flex-row">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute top-1/2 left-3.5 h-4 w-4 -translate-y-1/2 text-slate-400" />
            <input
              aria-label="Tìm món"
              className={`${INPUT_CLASS} pr-12 pl-10`}
              value={query}
              placeholder="Tìm món nhanh (tên món, viết tắt: cfsd, tdcs)..."
              onChange={(e) => setQuery(e.target.value)}
            />
            {query && (
              <button
                type="button"
                aria-label="Xóa tìm kiếm"
                onClick={() => setQuery("")}
                className="absolute top-0 right-0 flex h-12 w-12 items-center justify-center text-slate-400 hover:text-slate-600"
              >
                <X className="h-4 w-4" />
              </button>
            )}
          </div>
          <select
            aria-label="Lọc theo danh mục"
            className={`${INPUT_CLASS} sm:w-56`}
            value={categoryId}
            onChange={(e) => setCategoryId(e.target.value)}
          >
            <option value="all">Tất cả danh mục</option>
            {model.categories.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </div>
        <button
          type="button"
          disabled={!canAdd}
          title={canAdd ? undefined : "Tạo danh mục trước"}
          onClick={() => onOpenItem(null)}
          className="flex h-12 min-h-[48px] items-center justify-center gap-2 rounded-xl bg-emerald-600 px-5 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 active:scale-[0.98] disabled:opacity-50 sm:text-sm"
        >
          <Plus className="h-4 w-4" />+ THÊM MÓN MỚI (Ctrl+N)
        </button>
      </div>

      <p className="text-xs text-slate-500 dark:text-muted-foreground">
        Hiển thị: <span className="font-mono font-bold text-slate-900 dark:text-foreground">{visible.length} món</span>
      </p>

      {model.items.length === 0 ? (
        <p className="rounded-2xl border border-dashed border-slate-300 p-10 text-center text-sm text-slate-500 dark:border-border">
          Chưa có món nào trong thực đơn
        </p>
      ) : visible.length === 0 ? (
        <p className="rounded-2xl border border-dashed border-slate-300 p-10 text-center text-sm text-slate-500 dark:border-border">
          Không tìm thấy món phù hợp
        </p>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {visible.map((item) => (
            <ItemCard
              key={item.id}
              item={item}
              categoryName={categoryName.get(item.categoryId) ?? ""}
              groupCount={
                effectiveGroupIds(inheritedGroupIds(model.categories, item.categoryId), item.directGroupIds, item.excludedGroupIds)
                  .length
              }
              onOpen={() => onOpenItem(item.id)}
            />
          ))}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 5: Run the test and type-check**

Run: `cd web && bun test src/features/catalog/components/item-card.test.tsx && bunx tsc -b`
Expected: PASS; tsc exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/catalog/components/item-card.tsx web/src/features/catalog/components/item-card.test.tsx web/src/features/catalog/components/items-view.tsx
git commit -m "feat(web): catalog items view"
```

---

### Task 12: Categories view and modal

**Files:**
- Create: `web/src/features/catalog/lib/category-icons.ts`
- Create: `web/src/features/catalog/components/category-form-panel.tsx`
- Create: `web/src/features/catalog/components/category-form-modal.tsx`
- Create: `web/src/features/catalog/components/categories-view.tsx`
- Test: `web/src/features/catalog/lib/category-icons.test.ts`
- Test: `web/src/features/catalog/components/category-form-panel.test.tsx`

**Interfaces:**
- Consumes: Tasks 1, 2, 4, 5, 9. `StockToast` and `StockToastMessage` from `@/features/settings/components/stock-toast`.
- Produces: `CATEGORY_ICONS: Record<string, LucideIcon>`, `CATEGORY_ICON_NAMES: string[]`, `categoryIcon(name: string | null): LucideIcon`; `CategoryFormPanel(props)`; `CategoryFormModal({ snapshot, model, onClose, onCreated, onSaved })`; `CategoriesView({ model, onToast })`.

- [ ] **Step 1: Write the failing tests**

```ts
// web/src/features/catalog/lib/category-icons.test.ts
import { describe, expect, it } from "bun:test";
import { LayoutGrid } from "lucide-react";
import { CATEGORY_ICON_NAMES, categoryIcon } from "./category-icons";

describe("category icons", () => {
  it("offers only names the server accepts", () => {
    for (const name of CATEGORY_ICON_NAMES) expect(name).toMatch(/^[a-z0-9-]{1,40}$/);
  });

  it("covers every icon the dev seed uses", () => {
    for (const name of ["coffee", "bean", "leaf", "citrus", "milk", "glass-water", "cherry", "apple", "cup-soda", "ice-cream-bowl", "croissant"]) {
      expect(CATEGORY_ICON_NAMES).toContain(name);
    }
  });

  it("falls back for an unknown or missing icon", () => {
    expect(categoryIcon(null)).toBe(LayoutGrid);
    expect(categoryIcon("unknown")).toBe(LayoutGrid);
  });
});
```

```tsx
// web/src/features/catalog/components/category-form-panel.test.tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { CategoryFormPanel } from "./category-form-panel";

describe("CategoryFormPanel", () => {
  it("renders the fields, default-group chips, and footer", () => {
    const html = renderToString(
      <CategoryFormPanel
        form={{ name: "Trà", icon: "leaf", displayOrder: 2, groupIds: ["g-ice"] }}
        errors={{ displayOrder: "Thứ tự hiển thị từ 0 đến 9999" }}
        isEdit
        busy={false}
        results={null}
        groups={[{ id: "g-ice", name: "Mức đá", min: 1, max: 1, options: [], defaultOptionIds: [] }]}
        onChange={() => {}}
        onSave={() => {}}
        onClose={() => {}}
        onRetire={() => {}}
      />,
    );
    expect(html).toContain("Tên danh mục");
    expect(html).toContain("Biểu tượng danh mục (Icon)");
    expect(html).toContain("Mức đá");
    expect(html).toContain("Thứ tự hiển thị từ 0 đến 9999");
    expect(html).toContain("Xóa danh mục");
    expect(html).toContain("LƯU DANH MỤC");
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun test src/features/catalog/lib/category-icons.test.ts src/features/catalog/components/category-form-panel.test.tsx`
Expected: FAIL, modules not found.

- [ ] **Step 3: Write `category-icons.ts`**

```ts
// web/src/features/catalog/lib/category-icons.ts
import {
  Apple,
  Bean,
  Beer,
  Cake,
  Cherry,
  Citrus,
  Coffee,
  Cookie,
  Croissant,
  CupSoda,
  GlassWater,
  IceCreamBowl,
  IceCreamCone,
  LayoutGrid,
  Leaf,
  Milk,
  Nut,
  Sandwich,
  Soup,
  UtensilsCrossed,
  Wine,
  type LucideIcon,
} from "lucide-react";

/** The web owns the offered set (BA-1 §2.2); keys are stored as the category's icon. */
export const CATEGORY_ICONS: Record<string, LucideIcon> = {
  coffee: Coffee,
  bean: Bean,
  leaf: Leaf,
  citrus: Citrus,
  milk: Milk,
  "glass-water": GlassWater,
  cherry: Cherry,
  apple: Apple,
  "cup-soda": CupSoda,
  "ice-cream-bowl": IceCreamBowl,
  "ice-cream-cone": IceCreamCone,
  croissant: Croissant,
  cake: Cake,
  cookie: Cookie,
  sandwich: Sandwich,
  soup: Soup,
  nut: Nut,
  wine: Wine,
  beer: Beer,
  "utensils-crossed": UtensilsCrossed,
};

export const CATEGORY_ICON_NAMES = Object.keys(CATEGORY_ICONS);

export function categoryIcon(name: string | null): LucideIcon {
  return (name && CATEGORY_ICONS[name]) || LayoutGrid;
}
```

- [ ] **Step 4: Write `category-form-panel.tsx`**

```tsx
// web/src/features/catalog/components/category-form-panel.tsx
import type { ReactElement } from "react";
import { LayoutGrid } from "lucide-react";
import type { CatGroup } from "../lib/catalog-model";
import { CATEGORY_ICON_NAMES, CATEGORY_ICONS } from "../lib/category-icons";
import type { CategoryForm } from "../lib/forms";
import type { StepResult } from "../lib/run-plan";
import type { FieldErrors } from "../lib/validation";
import { Chip, Field, FormFooter, INPUT_CLASS, ModalFrame, readNumber } from "./form-bits";
import { SaveProgress } from "./save-progress";

export interface CategoryFormPanelProps {
  form: CategoryForm;
  errors: FieldErrors;
  isEdit: boolean;
  busy: boolean;
  results: StepResult[] | null;
  groups: CatGroup[];
  onChange: (next: CategoryForm) => void;
  onSave: () => void;
  onClose: () => void;
  onRetire?: () => void;
}

export function CategoryFormPanel(p: CategoryFormPanelProps): ReactElement {
  const { form, errors, onChange } = p;
  const toggleGroup = (id: string) =>
    onChange({
      ...form,
      groupIds: form.groupIds.includes(id) ? form.groupIds.filter((g) => g !== id) : [...form.groupIds, id],
    });
  return (
    <ModalFrame
      titleId="category-form-title"
      icon={LayoutGrid}
      title={p.isEdit ? "Chỉnh sửa danh mục" : "Thêm danh mục mới"}
      subtitle="Thiết lập phân loại món, thứ tự hiển thị và nhóm tùy chọn mặc định."
      busy={p.busy}
      onClose={p.onClose}
      onSave={p.onSave}
      footer={
        <FormFooter
          retireLabel="Xóa danh mục"
          onRetire={p.onRetire}
          onClose={p.onClose}
          onSave={p.onSave}
          saveLabel="LƯU DANH MỤC"
          busy={p.busy}
        />
      }
    >
      <div className="space-y-4">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_140px]">
          <Field label="Tên danh mục" htmlFor="category-name" required error={errors.name}>
            <input
              id="category-name"
              className={INPUT_CLASS}
              value={form.name}
              placeholder="Ví dụ: Cà phê Việt, Trà & Macchiato..."
              onChange={(e) => onChange({ ...form, name: e.target.value })}
            />
          </Field>
          <Field label="Thứ tự hiển thị" htmlFor="category-order" error={errors.displayOrder}>
            <input
              id="category-order"
              type="number"
              min={0}
              max={9999}
              className={`${INPUT_CLASS} font-mono`}
              value={form.displayOrder}
              onChange={(e) => onChange({ ...form, displayOrder: readNumber(e.target.value) })}
            />
          </Field>
        </div>
        <div className="space-y-1.5">
          <p className="text-xs font-bold text-slate-700 dark:text-foreground">Biểu tượng danh mục (Icon)</p>
          <p className="text-[11px] text-slate-400">Chọn một biểu tượng phù hợp với nhóm đồ uống hoặc món ăn.</p>
          <div className="flex flex-wrap gap-2">
            <Chip active={form.icon === null} onClick={() => onChange({ ...form, icon: null })}>
              Không có
            </Chip>
            {CATEGORY_ICON_NAMES.map((name) => {
              const Icon = CATEGORY_ICONS[name];
              return (
                <Chip key={name} active={form.icon === name} title={name} onClick={() => onChange({ ...form, icon: name })}>
                  <Icon className="h-4 w-4" aria-label={name} />
                </Chip>
              );
            })}
          </div>
        </div>
        <div className="space-y-1.5">
          <p className="text-xs font-bold text-slate-700 dark:text-foreground">Nhóm tùy chọn mặc định (kế thừa tự động cho món mới)</p>
          <p className="text-[11px] text-slate-400">Các món ăn thuộc danh mục này sẽ tự động kế thừa các nhóm tùy chọn được tích chọn.</p>
          <div className="flex flex-wrap gap-2">
            {p.groups.map((g) => (
              <Chip key={g.id} active={form.groupIds.includes(g.id)} onClick={() => toggleGroup(g.id)}>
                {g.name}
              </Chip>
            ))}
          </div>
        </div>
        <SaveProgress results={p.results} />
      </div>
    </ModalFrame>
  );
}
```

- [ ] **Step 5: Write `category-form-modal.tsx`**

```tsx
// web/src/features/catalog/components/category-form-modal.tsx
import { useState, type ReactElement } from "react";
import { playErrorBuzz } from "@/lib/sound";
import { useCatalogSave, useRetireEntity } from "../api/use-catalog-admin";
import type { CatalogModel, CatCategory } from "../lib/catalog-model";
import { categoryFormFrom } from "../lib/forms";
import { planCategorySave } from "../lib/save-plan";
import { hasErrors, validateCategoryForm, type FieldErrors } from "../lib/validation";
import { CategoryFormPanel } from "./category-form-panel";
import { RetireDialog } from "./retire-dialog";

export interface CategoryFormModalProps {
  snapshot: CatCategory | null;
  model: CatalogModel;
  onClose: () => void;
  onCreated: (categoryId: string) => void;
  onSaved: (message: string) => void;
}

export function CategoryFormModal({ snapshot, model, onClose, onCreated, onSaved }: CategoryFormModalProps): ReactElement {
  const [form, setForm] = useState(() => categoryFormFrom(snapshot, model.categories));
  const [errors, setErrors] = useState<FieldErrors>({});
  const [retiring, setRetiring] = useState(false);
  const { save, isSaving, results } = useCatalogSave();
  const retireEntity = useRetireEntity();

  const handleSave = async () => {
    if (isSaving) return;
    const found = validateCategoryForm(form);
    setErrors(found);
    if (hasErrors(found)) {
      playErrorBuzz();
      return;
    }
    const outcome = await save(planCategorySave(snapshot, form));
    if (outcome.cancelled) return;
    if (outcome.ok) {
      onSaved(`Đã lưu danh mục ${form.name.trim()}`);
      onClose();
      return;
    }
    if (!snapshot && outcome.created.categoryId) onCreated(outcome.created.categoryId);
  };

  return (
    <>
      <CategoryFormPanel
        form={form}
        errors={errors}
        isEdit={snapshot !== null}
        busy={isSaving || retiring}
        results={results}
        groups={model.groups}
        onChange={setForm}
        onSave={() => void handleSave()}
        onClose={onClose}
        onRetire={snapshot ? () => setRetiring(true) : undefined}
      />
      <RetireDialog
        open={retiring}
        name={snapshot?.name ?? ""}
        onClose={() => setRetiring(false)}
        onConfirm={async (retirement) => {
          if (!snapshot) return null;
          const failure = await retireEntity("category", snapshot.id, snapshot.name, retirement);
          if (failure) return failure;
          setRetiring(false);
          onSaved(`Đã ngừng bán ${snapshot.name}`);
          onClose();
          return null;
        }}
      />
    </>
  );
}
```

- [ ] **Step 6: Write `categories-view.tsx`**

```tsx
// web/src/features/catalog/components/categories-view.tsx
import { useRef, useState, type ReactElement } from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";
import type { StockToastMessage } from "@/features/settings/components/stock-toast";
import { useRetireEntity } from "../api/use-catalog-admin";
import type { CatalogModel, CatCategory } from "../lib/catalog-model";
import { categoryIcon } from "../lib/category-icons";
import { CategoryFormModal } from "./category-form-modal";
import { RetireDialog } from "./retire-dialog";

export interface CategoriesViewProps {
  model: CatalogModel;
  onToast: (toast: StockToastMessage) => void;
}

export function CategoriesView({ model, onToast }: CategoriesViewProps): ReactElement {
  const [editing, setEditing] = useState<{ id: string | null; session: number } | null>(null);
  const [retiring, setRetiring] = useState<CatCategory | null>(null);
  const sessions = useRef(0);
  const retireEntity = useRetireEntity();
  const groupName = new Map(model.groups.map((g) => [g.id, g.name]));
  const open = (id: string | null) => setEditing({ id, session: ++sessions.current });

  return (
    <div className="space-y-5">
      <div className="flex flex-col justify-between gap-3 rounded-2xl border border-slate-200 bg-card p-4 shadow-2xs sm:flex-row sm:items-center dark:border-border">
        <div>
          <h2 className="text-sm font-bold text-slate-900 sm:text-base dark:text-foreground">Danh mục Món ăn & Đồ uống</h2>
          <p className="text-xs text-slate-500 dark:text-muted-foreground">
            Quản lý nhóm phân loại thực đơn, thứ tự hiển thị trên quầy thu ngân và cấu hình nhóm tùy chọn kế thừa mặc định.
          </p>
        </div>
        <button
          type="button"
          onClick={() => open(null)}
          className="flex h-12 min-h-[48px] shrink-0 items-center justify-center gap-2 rounded-xl bg-emerald-600 px-5 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 active:scale-[0.98] sm:text-sm"
        >
          <Plus className="h-4 w-4" />+ THÊM DANH MỤC
        </button>
      </div>

      <p className="text-xs text-slate-500 dark:text-muted-foreground">
        Hiển thị: <span className="font-mono font-bold text-slate-900 dark:text-foreground">{model.categories.length} danh mục</span>
      </p>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {model.categories.map((c) => {
          const Icon = categoryIcon(c.icon);
          const count = model.items.filter((i) => i.categoryId === c.id).length;
          return (
            <div key={c.id} className="flex flex-col gap-3 rounded-2xl border border-slate-200 bg-card p-4 shadow-2xs dark:border-border">
              <div className="flex items-start justify-between gap-3">
                <div className="flex items-center gap-3">
                  <div className="flex h-11 w-11 items-center justify-center rounded-xl border border-emerald-200 bg-emerald-50 text-emerald-600 dark:border-emerald-900 dark:bg-emerald-950/40">
                    <Icon className="h-5 w-5" />
                  </div>
                  <div>
                    <p className="text-sm font-bold text-slate-900 dark:text-foreground">{c.name}</p>
                    <p className="text-[11px] text-slate-500">
                      Thứ tự {c.displayOrder} · {count} món
                    </p>
                  </div>
                </div>
                <div className="flex gap-1.5">
                  <button
                    type="button"
                    aria-label={`Sửa ${c.name}`}
                    onClick={() => open(c.id)}
                    className="flex h-12 min-h-[48px] w-12 min-w-[48px] items-center justify-center rounded-xl border border-slate-200 text-slate-600 hover:bg-slate-50 dark:border-border"
                  >
                    <Pencil className="h-4 w-4" />
                  </button>
                  <button
                    type="button"
                    aria-label={`Ngừng bán ${c.name}`}
                    onClick={() => setRetiring(c)}
                    className="flex h-12 min-h-[48px] w-12 min-w-[48px] items-center justify-center rounded-xl border border-rose-200 bg-rose-50 text-rose-700 hover:bg-rose-100 dark:border-rose-900 dark:bg-rose-950/30"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </div>
              <div className="flex flex-wrap gap-1.5">
                {c.groupIds.length === 0 ? (
                  <span className="text-[11px] text-slate-400">Chưa có nhóm mặc định</span>
                ) : (
                  c.groupIds.map((id) => (
                    <span key={id} className="rounded-lg bg-slate-100 px-2 py-1 text-[11px] font-semibold text-slate-600 dark:bg-muted dark:text-muted-foreground">
                      {groupName.get(id)}
                    </span>
                  ))
                )}
              </div>
            </div>
          );
        })}
      </div>

      {editing && (
        <CategoryFormModal
          key={editing.session}
          snapshot={model.categories.find((c) => c.id === editing.id) ?? null}
          model={model}
          onClose={() => setEditing(null)}
          onCreated={(id) => setEditing((e) => (e ? { ...e, id } : e))}
          onSaved={(text) => onToast({ text, tone: "success" })}
        />
      )}
      <RetireDialog
        open={retiring !== null}
        name={retiring?.name ?? ""}
        onClose={() => setRetiring(null)}
        onConfirm={async (retirement) => {
          if (!retiring) return null;
          const failure = await retireEntity("category", retiring.id, retiring.name, retirement);
          if (failure) return failure;
          onToast({ text: `Đã ngừng bán ${retiring.name}`, tone: "success" });
          setRetiring(null);
          return null;
        }}
      />
    </div>
  );
}
```

- [ ] **Step 7: Run tests and type-check**

Run: `cd web && bun test src/features/catalog && bunx tsc -b`
Expected: PASS; tsc exit 0.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/catalog/lib/category-icons.ts web/src/features/catalog/lib/category-icons.test.ts web/src/features/catalog/components/category-form-panel.tsx web/src/features/catalog/components/category-form-panel.test.tsx web/src/features/catalog/components/category-form-modal.tsx web/src/features/catalog/components/categories-view.tsx
git commit -m "feat(web): catalog categories view and modal"
```

---

### Task 13: Modifier groups view and modal

**Files:**
- Create: `web/src/features/catalog/components/group-form-panel.tsx`
- Create: `web/src/features/catalog/components/group-form-modal.tsx`
- Create: `web/src/features/catalog/components/groups-view.tsx`
- Test: `web/src/features/catalog/components/group-form-panel.test.tsx`

**Interfaces:**
- Consumes: Tasks 1, 2, 4, 5, 9. `blankOptionRow`, `toggleDefault`, `selectionType`, `setSelectionType`, `removeOptionRow`, `groupFormFrom` (Task 2); `ruleLabel`, `currentAssignment` (Task 1).
- Produces: `GroupFormPanel(props)` with `focusKey`, `onAddRow`, `onRemoveRow` props; `GroupFormModal({ snapshot, onClose, onSaved })`; `GroupsView({ model, onToast })`.

- [ ] **Step 1: Write the failing test**

```tsx
// web/src/features/catalog/components/group-form-panel.test.tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { GroupFormPanel, type GroupFormPanelProps } from "./group-form-panel";

function props(patch: Partial<GroupFormPanelProps>): GroupFormPanelProps {
  return {
    form: {
      name: "Mức đá",
      min: 1,
      max: 1,
      rows: [{ key: "a", id: "o-a", name: "100% đá", surchargeVnd: 0, isDefault: true }],
      retiredOptions: [],
    },
    errors: {},
    isEdit: true,
    busy: false,
    results: null,
    focusKey: null,
    onChange: () => {},
    onAddRow: () => {},
    onRemoveRow: () => {},
    onSave: () => {},
    onClose: () => {},
    ...patch,
  };
}

describe("GroupFormPanel", () => {
  it("renders the rule choice, rows, and footer", () => {
    const html = renderToString(<GroupFormPanel {...props({ onRetire: () => {} })} />);
    expect(html).toContain("Chọn 1 duy nhất (Radio)");
    expect(html).toContain("Chọn nhiều (Checkbox)");
    expect(html).toContain("100% đá");
    expect(html).toContain("+ Thêm dòng Topping");
    expect(html).toContain("Xóa nhóm topping");
    expect(html).toContain("LƯU NHÓM TOPPING");
  });

  it("shows rule errors", () => {
    const html = renderToString(
      <GroupFormPanel {...props({ errors: { defaults: "Số lựa chọn mặc định phải từ tối thiểu đến tối đa" } })} />,
    );
    expect(html).toContain("Số lựa chọn mặc định phải từ tối thiểu đến tối đa");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/catalog/components/group-form-panel.test.tsx`
Expected: FAIL, module not found.

- [ ] **Step 3: Write `group-form-panel.tsx`**

```tsx
// web/src/features/catalog/components/group-form-panel.tsx
import type { ReactElement } from "react";
import { Layers, Plus, Trash2 } from "lucide-react";
import { selectionType, setSelectionType, toggleDefault, type GroupForm, type OptionRow } from "../lib/forms";
import type { StepResult } from "../lib/run-plan";
import type { FieldErrors } from "../lib/validation";
import { Chip, Field, FormFooter, INPUT_CLASS, ModalFrame, MONEY_INPUT_CLASS, readNumber } from "./form-bits";
import { SaveProgress } from "./save-progress";

export interface GroupFormPanelProps {
  form: GroupForm;
  errors: FieldErrors;
  isEdit: boolean;
  busy: boolean;
  results: StepResult[] | null;
  /** The row whose name input takes focus when it mounts (Enter-to-add). */
  focusKey: string | null;
  onChange: (next: GroupForm) => void;
  onAddRow: () => void;
  onRemoveRow: (key: string) => void;
  onSave: () => void;
  onClose: () => void;
  onRetire?: () => void;
}

const ERROR_TEXT = "text-[11px] font-semibold text-rose-600";

export function GroupFormPanel(p: GroupFormPanelProps): ReactElement {
  const { form, errors, onChange } = p;
  const type = selectionType(form);
  const setRow = (key: string, patch: Partial<OptionRow>) =>
    onChange({ ...form, rows: form.rows.map((r) => (r.key === key ? { ...r, ...patch } : r)) });

  return (
    <ModalFrame
      titleId="group-form-title"
      icon={Layers}
      title={p.isEdit ? "Chỉnh sửa nhóm Topping" : "Tạo nhóm Topping / Tùy chọn mới"}
      subtitle="Cấu hình quy tắc chọn (đơn / đa chọn), giới hạn số lượng và danh sách tùy chọn con kèm giá."
      busy={p.busy}
      onClose={p.onClose}
      onSave={p.onSave}
      footer={
        <FormFooter
          retireLabel="Xóa nhóm topping"
          onRetire={p.onRetire}
          onClose={p.onClose}
          onSave={p.onSave}
          saveLabel="LƯU NHÓM TOPPING"
          busy={p.busy}
        />
      }
    >
      <div className="space-y-4">
        <Field label="Tên nhóm tùy chọn / Topping" htmlFor="group-name" required error={errors.name}>
          <input
            id="group-name"
            className={INPUT_CLASS}
            value={form.name}
            placeholder="Ví dụ: Topping Trà Trái Cây, Lớp Kem Cheese, Mức đường..."
            onChange={(e) => onChange({ ...form, name: e.target.value })}
          />
        </Field>

        <div className="space-y-1.5">
          <p className="text-xs font-bold text-slate-700 dark:text-foreground">Quy tắc chọn của khách hàng</p>
          <div className="grid grid-cols-2 gap-2">
            <Chip active={type === "single"} onClick={() => onChange(setSelectionType(form, "single"))}>
              Chọn 1 duy nhất (Radio)
            </Chip>
            <Chip active={type === "multiple"} onClick={() => onChange(setSelectionType(form, "multiple"))}>
              Chọn nhiều (Checkbox)
            </Chip>
          </div>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <Field label="Số lượng chọn tối thiểu" htmlFor="group-min" error={errors.min} hint="Đặt 0 nếu không bắt buộc; đặt 1 nếu bắt buộc phải chọn.">
            <input
              id="group-min"
              type="number"
              min={0}
              className={`${INPUT_CLASS} font-mono`}
              value={form.min}
              onChange={(e) => onChange({ ...form, min: readNumber(e.target.value) })}
            />
          </Field>
          <Field label="Số lượng chọn tối đa" htmlFor="group-max" error={errors.max} hint="Giới hạn số topping tối đa được thêm.">
            <input
              id="group-max"
              type="number"
              min={1}
              disabled={type === "single"}
              className={`${INPUT_CLASS} font-mono disabled:bg-slate-100 disabled:text-slate-400`}
              value={form.max}
              onChange={(e) => onChange({ ...form, max: readNumber(e.target.value) })}
            />
          </Field>
        </div>

        <div className="space-y-2">
          <div>
            <p className="text-xs font-bold text-slate-700 dark:text-foreground">Danh sách lựa chọn / Topping con</p>
            <p className="text-[11px] text-slate-400">Nhấn Enter tại ô giá để tự động thêm dòng mới và chuyển con trỏ.</p>
          </div>
          {form.rows.map((row) => (
            <div key={row.key} className="space-y-1">
              <div className="flex items-center gap-2">
                <input
                  aria-label="Tên lựa chọn"
                  autoFocus={row.key === p.focusKey}
                  className={INPUT_CLASS}
                  value={row.name}
                  placeholder="Trân châu trắng"
                  onChange={(e) => setRow(row.key, { name: e.target.value })}
                />
                <input
                  aria-label="Giá thêm"
                  type="number"
                  min={0}
                  step={1000}
                  className={`${MONEY_INPUT_CLASS} w-36 shrink-0`}
                  value={row.surchargeVnd}
                  onChange={(e) => setRow(row.key, { surchargeVnd: readNumber(e.target.value) })}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      p.onAddRow();
                    }
                  }}
                />
                <Chip active={row.isDefault} onClick={() => onChange(toggleDefault(form, row.key))}>
                  Mặc định
                </Chip>
                <button
                  type="button"
                  aria-label="Xóa dòng này"
                  disabled={form.rows.length <= 1}
                  onClick={() => p.onRemoveRow(row.key)}
                  className="flex h-12 min-h-[48px] w-12 min-w-[48px] shrink-0 items-center justify-center rounded-lg border border-slate-200 text-slate-400 transition hover:bg-rose-50 hover:text-rose-600 disabled:pointer-events-none disabled:opacity-40 dark:border-border"
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
              {errors[`row.${row.key}`] && (
                <p role="alert" className={ERROR_TEXT}>
                  {errors[`row.${row.key}`]}
                </p>
              )}
            </div>
          ))}
          {[errors.rows, errors.defaults].filter(Boolean).map((text) => (
            <p key={text} role="alert" className={ERROR_TEXT}>
              {text}
            </p>
          ))}
          <button
            type="button"
            onClick={p.onAddRow}
            className="flex h-12 min-h-[48px] items-center gap-1.5 rounded-xl border border-dashed border-emerald-300 bg-white px-4 text-xs font-bold text-emerald-700 transition hover:bg-emerald-50 active:scale-[0.98] dark:bg-card"
          >
            <Plus className="h-4 w-4" />+ Thêm dòng Topping
          </button>
        </div>
        <SaveProgress results={p.results} />
      </div>
    </ModalFrame>
  );
}
```

- [ ] **Step 4: Write `group-form-modal.tsx`**

```tsx
// web/src/features/catalog/components/group-form-modal.tsx
import { useState, type ReactElement } from "react";
import { playErrorBuzz } from "@/lib/sound";
import { useCatalogSave, useRetireEntity } from "../api/use-catalog-admin";
import type { CatGroup } from "../lib/catalog-model";
import { blankOptionRow, groupFormFrom, removeOptionRow } from "../lib/forms";
import { planGroupSave } from "../lib/save-plan";
import { hasErrors, validateGroupForm, type FieldErrors } from "../lib/validation";
import { GroupFormPanel } from "./group-form-panel";
import { RetireDialog } from "./retire-dialog";

export interface GroupFormModalProps {
  snapshot: CatGroup | null;
  onClose: () => void;
  onSaved: (message: string) => void;
}

type Retiring = { kind: "group" } | { kind: "option"; key: string; name: string } | null;

export function GroupFormModal({ snapshot, onClose, onSaved }: GroupFormModalProps): ReactElement {
  const [form, setForm] = useState(() => groupFormFrom(snapshot));
  const [errors, setErrors] = useState<FieldErrors>({});
  const [focusKey, setFocusKey] = useState<string | null>(null);
  const [retiring, setRetiring] = useState<Retiring>(null);
  const { save, isSaving, results } = useCatalogSave();
  const retireEntity = useRetireEntity();

  const handleSave = async () => {
    if (isSaving) return;
    const found = validateGroupForm(form);
    setErrors(found);
    if (hasErrors(found)) {
      playErrorBuzz();
      return;
    }
    // Group create is a single step, so a failure leaves nothing to resume.
    const outcome = await save(planGroupSave(snapshot, form));
    if (outcome.ok && !outcome.cancelled) {
      onSaved(`Đã lưu nhóm ${form.name.trim()}`);
      onClose();
    }
  };

  const addRow = () => {
    const row = blankOptionRow();
    setForm((f) => ({ ...f, rows: [...f.rows, row] }));
    setFocusKey(row.key);
  };

  const removeRow = (key: string) => {
    const row = form.rows.find((r) => r.key === key);
    if (!row) return;
    if (row.id) setRetiring({ kind: "option", key, name: row.name });
    else setForm((f) => removeOptionRow(f, key, null));
  };

  return (
    <>
      <GroupFormPanel
        form={form}
        errors={errors}
        isEdit={snapshot !== null}
        busy={isSaving || retiring !== null}
        results={results}
        focusKey={focusKey}
        onChange={setForm}
        onAddRow={addRow}
        onRemoveRow={removeRow}
        onSave={() => void handleSave()}
        onClose={onClose}
        onRetire={snapshot ? () => setRetiring({ kind: "group" }) : undefined}
      />
      <RetireDialog
        open={retiring !== null}
        name={retiring?.kind === "option" ? retiring.name : form.name}
        onClose={() => setRetiring(null)}
        onConfirm={async (retirement) => {
          if (retiring?.kind === "option") {
            setForm((f) => removeOptionRow(f, retiring.key, retirement));
            setRetiring(null);
            return null;
          }
          if (!snapshot) return null;
          const failure = await retireEntity("group", snapshot.id, snapshot.name, retirement);
          if (failure) return failure;
          setRetiring(null);
          onSaved(`Đã ngừng bán ${snapshot.name}`);
          onClose();
          return null;
        }}
      />
    </>
  );
}
```

- [ ] **Step 5: Write `groups-view.tsx`**

```tsx
// web/src/features/catalog/components/groups-view.tsx
import { useRef, useState, type ReactElement } from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";
import type { StockToastMessage } from "@/features/settings/components/stock-toast";
import { formatVND } from "@/lib/utils";
import { useRetireEntity } from "../api/use-catalog-admin";
import { currentAssignment, ruleLabel, type CatalogModel, type CatGroup } from "../lib/catalog-model";
import { GroupFormModal } from "./group-form-modal";
import { RetireDialog } from "./retire-dialog";

export interface GroupsViewProps {
  model: CatalogModel;
  onToast: (toast: StockToastMessage) => void;
}

export function GroupsView({ model, onToast }: GroupsViewProps): ReactElement {
  const [editing, setEditing] = useState<{ id: string | null; session: number } | null>(null);
  const [retiring, setRetiring] = useState<CatGroup | null>(null);
  const sessions = useRef(0);
  const retireEntity = useRetireEntity();
  const open = (id: string | null) => setEditing({ id, session: ++sessions.current });

  return (
    <div className="space-y-5">
      <div className="flex flex-col justify-between gap-3 rounded-2xl border border-slate-200 bg-card p-4 shadow-2xs sm:flex-row sm:items-center dark:border-border">
        <div>
          <h2 className="text-sm font-bold text-slate-900 sm:text-base dark:text-foreground">Nhóm Topping & Tùy chọn Món</h2>
          <p className="text-xs text-slate-500 dark:text-muted-foreground">
            Thiết lập các nhóm tùy chọn (Đường, Đá, Topping, Kem Phô Mai), số lượng chọn tối thiểu/tối đa và đơn giá từng loại.
          </p>
        </div>
        <button
          type="button"
          onClick={() => open(null)}
          className="flex h-12 min-h-[48px] shrink-0 items-center justify-center gap-2 rounded-xl bg-emerald-600 px-5 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 active:scale-[0.98] sm:text-sm"
        >
          <Plus className="h-4 w-4" />+ TẠO NHÓM TOPPING
        </button>
      </div>

      <p className="text-xs text-slate-500 dark:text-muted-foreground">
        Hiển thị: <span className="font-mono font-bold text-slate-900 dark:text-foreground">{model.groups.length} nhóm</span>
      </p>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {model.groups.map((g) => {
          const usage = currentAssignment(model, g.id);
          return (
            <div key={g.id} className="flex flex-col gap-3 rounded-2xl border border-slate-200 bg-card p-4 shadow-2xs dark:border-border">
              <div className="flex items-start justify-between gap-3">
                <div>
                  <p className="text-sm font-bold text-slate-900 dark:text-foreground">{g.name}</p>
                  <p className="text-[11px] text-slate-500">
                    {ruleLabel(g)} · Dùng cho {usage.itemIds.length} món · {usage.categoryIds.length} danh mục
                  </p>
                </div>
                <div className="flex gap-1.5">
                  <button
                    type="button"
                    aria-label={`Sửa ${g.name}`}
                    onClick={() => open(g.id)}
                    className="flex h-12 min-h-[48px] w-12 min-w-[48px] items-center justify-center rounded-xl border border-slate-200 text-slate-600 hover:bg-slate-50 dark:border-border"
                  >
                    <Pencil className="h-4 w-4" />
                  </button>
                  <button
                    type="button"
                    aria-label={`Ngừng bán ${g.name}`}
                    onClick={() => setRetiring(g)}
                    className="flex h-12 min-h-[48px] w-12 min-w-[48px] items-center justify-center rounded-xl border border-rose-200 bg-rose-50 text-rose-700 hover:bg-rose-100 dark:border-rose-900 dark:bg-rose-950/30"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </div>
              <ul className="space-y-1">
                {g.options.map((o) => (
                  <li key={o.id} className="flex items-center justify-between text-xs">
                    <span className="text-slate-700 dark:text-foreground">
                      {o.name}
                      {g.defaultOptionIds.includes(o.id) && <span className="ml-1.5 text-[10px] font-bold text-emerald-600">Mặc định</span>}
                    </span>
                    <span className="font-mono text-slate-500">{o.surchargeVnd > 0 ? `+${formatVND(o.surchargeVnd)}` : "0"}</span>
                  </li>
                ))}
              </ul>
            </div>
          );
        })}
      </div>

      {editing && (
        <GroupFormModal
          key={editing.session}
          snapshot={model.groups.find((g) => g.id === editing.id) ?? null}
          onClose={() => setEditing(null)}
          onSaved={(text) => onToast({ text, tone: "success" })}
        />
      )}
      <RetireDialog
        open={retiring !== null}
        name={retiring?.name ?? ""}
        onClose={() => setRetiring(null)}
        onConfirm={async (retirement) => {
          if (!retiring) return null;
          const failure = await retireEntity("group", retiring.id, retiring.name, retirement);
          if (failure) return failure;
          onToast({ text: `Đã ngừng bán ${retiring.name}`, tone: "success" });
          setRetiring(null);
          return null;
        }}
      />
    </div>
  );
}
```

- [ ] **Step 6: Run tests and type-check**

Run: `cd web && bun test src/features/catalog && bunx tsc -b`
Expected: PASS; tsc exit 0.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/catalog/components/group-form-panel.tsx web/src/features/catalog/components/group-form-panel.test.tsx web/src/features/catalog/components/group-form-modal.tsx web/src/features/catalog/components/groups-view.tsx
git commit -m "feat(web): catalog modifier groups view and modal"
```

---

### Task 14: Batch Linker

**Files:**
- Create: `web/src/features/catalog/lib/linker.ts`
- Create: `web/src/features/catalog/components/linker-view.tsx`
- Test: `web/src/features/catalog/lib/linker.test.ts`

**Interfaces:**
- Consumes: `Assignment`, `CatalogModel`, `CatItem`, `currentAssignment`, `filterItems`, `sameSet` (Task 1); `planAssignments` (Task 4); `useCatalogSave` (Task 5); `toSellablePreview` (Task 7); `ItemPickerBody` (Task 8).
- Produces: `LinkState = "excluded" | "direct" | "inherited" | "none"`, `linkState(item, groupId, draft)`, `toggleItem(draft, itemId)`, `toggleCategory(draft, categoryId)`, `selectAllInCategory(draft, model, groupId, categoryId)`, `clearCategoryItems(draft, model, categoryId)`, `sameAssignment(a, b)`; `LinkerView({ model, onToast })`.

- [ ] **Step 1: Write the failing test**

```ts
// web/src/features/catalog/lib/linker.test.ts
import { describe, expect, it } from "bun:test";
import type { CatalogModel, CatItem } from "./catalog-model";
import { clearCategoryItems, linkState, sameAssignment, selectAllInCategory, toggleCategory, toggleItem } from "./linker";

function item(id: string, categoryId: string, excluded: string[] = []): CatItem {
  return {
    id,
    categoryId,
    name: id,
    code: null,
    badge: null,
    description: null,
    imageUrl: null,
    priceVnd: 1000,
    sizes: [],
    directGroupIds: [],
    excludedGroupIds: excluded,
  };
}

const model: CatalogModel = {
  categories: [
    { id: "c-tea", name: "Trà", icon: null, displayOrder: 1, groupIds: ["g-ice"] },
    { id: "c-cake", name: "Bánh", icon: null, displayOrder: 2, groupIds: [] },
  ],
  items: [item("t1", "c-tea"), item("t2", "c-tea", ["g-ice"]), item("k1", "c-cake")],
  groups: [],
};

describe("linker", () => {
  it("classifies each item for the selected group", () => {
    const draft = { itemIds: ["k1"], categoryIds: ["c-tea"] };
    expect(linkState(model.items[0], "g-ice", draft)).toBe("inherited");
    expect(linkState(model.items[1], "g-ice", draft)).toBe("excluded");
    expect(linkState(model.items[2], "g-ice", draft)).toBe("direct");
    expect(linkState(model.items[2], "g-ice", { itemIds: [], categoryIds: [] })).toBe("none");
  });

  it("toggles items and categories", () => {
    const empty = { itemIds: [], categoryIds: [] };
    expect(toggleItem(empty, "k1")).toEqual({ itemIds: ["k1"], categoryIds: [] });
    expect(toggleItem({ itemIds: ["k1"], categoryIds: [] }, "k1")).toEqual(empty);
    expect(toggleCategory(empty, "c-tea")).toEqual({ itemIds: [], categoryIds: ["c-tea"] });
  });

  it("selects every item of a category except those excluding the group", () => {
    const next = selectAllInCategory({ itemIds: ["k1"], categoryIds: [] }, model, "g-ice", "c-tea");
    expect(next.itemIds.sort()).toEqual(["k1", "t1"]);
    expect(clearCategoryItems(next, model, "c-tea").itemIds).toEqual(["k1"]);
  });

  it("compares drafts as sets", () => {
    expect(sameAssignment({ itemIds: ["a", "b"], categoryIds: [] }, { itemIds: ["b", "a"], categoryIds: [] })).toBe(true);
    expect(sameAssignment({ itemIds: [], categoryIds: ["c"] }, { itemIds: [], categoryIds: [] })).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/catalog/lib/linker.test.ts`
Expected: FAIL, module not found.

- [ ] **Step 3: Write `linker.ts`**

```ts
// web/src/features/catalog/lib/linker.ts
import { sameSet, type Assignment, type CatalogModel, type CatItem } from "./catalog-model";

export type LinkState = "excluded" | "direct" | "inherited" | "none";

/**
 * An item excluding the group cannot be assigned directly (INVALID_INHERITANCE,
 * BA-1 §4.3); the Manager lifts the exclusion in the item form first.
 */
export function linkState(item: CatItem, groupId: string, draft: Assignment): LinkState {
  if (item.excludedGroupIds.includes(groupId)) return "excluded";
  if (draft.itemIds.includes(item.id)) return "direct";
  if (draft.categoryIds.includes(item.categoryId)) return "inherited";
  return "none";
}

function toggle(ids: readonly string[], id: string): string[] {
  return ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id];
}

export function toggleItem(draft: Assignment, itemId: string): Assignment {
  return { ...draft, itemIds: toggle(draft.itemIds, itemId) };
}

export function toggleCategory(draft: Assignment, categoryId: string): Assignment {
  return { ...draft, categoryIds: toggle(draft.categoryIds, categoryId) };
}

/** "Chọn hết": ticks every item of the category that does not exclude the group. */
export function selectAllInCategory(draft: Assignment, model: CatalogModel, groupId: string, categoryId: string): Assignment {
  const add = model.items
    .filter((i) => i.categoryId === categoryId && !i.excludedGroupIds.includes(groupId) && !draft.itemIds.includes(i.id))
    .map((i) => i.id);
  return { ...draft, itemIds: [...draft.itemIds, ...add] };
}

/** "Bỏ chọn hết": unticks every item of the category. */
export function clearCategoryItems(draft: Assignment, model: CatalogModel, categoryId: string): Assignment {
  const inCategory = new Set(model.items.filter((i) => i.categoryId === categoryId).map((i) => i.id));
  return { ...draft, itemIds: draft.itemIds.filter((id) => !inCategory.has(id)) };
}

export function sameAssignment(a: Assignment, b: Assignment): boolean {
  return sameSet(a.itemIds, b.itemIds) && sameSet(a.categoryIds, b.categoryIds);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && bun test src/features/catalog/lib/linker.test.ts`
Expected: PASS, 4 tests.

- [ ] **Step 5: Write `linker-view.tsx`**

```tsx
// web/src/features/catalog/components/linker-view.tsx
import { useState, type ReactElement } from "react";
import { MonitorSmartphone, Search, Zap } from "lucide-react";
import { useHotkeys } from "react-hotkeys-hook";
import type { StockToastMessage } from "@/features/settings/components/stock-toast";
import { ItemPickerBody } from "@/features/pos/components/item-picker-dialog";
import { cn } from "@/lib/utils";
import { useCatalogSave } from "../api/use-catalog-admin";
import { currentAssignment, filterItems, type Assignment, type CatalogModel } from "../lib/catalog-model";
import { clearCategoryItems, linkState, sameAssignment, selectAllInCategory, toggleCategory, toggleItem } from "../lib/linker";
import { planAssignments } from "../lib/save-plan";
import { toSellablePreview } from "../lib/sellable-preview";
import { INPUT_CLASS } from "./form-bits";

export interface LinkerViewProps {
  model: CatalogModel;
  onToast: (toast: StockToastMessage) => void;
}

const STATE_LABEL = { excluded: "Đang loại trừ, bỏ loại trừ trong form Món", inherited: "Kế thừa", direct: "", none: "" } as const;

export function LinkerView({ model, onToast }: LinkerViewProps): ReactElement {
  const [groupId, setGroupId] = useState<string | null>(null);
  const [draft, setDraft] = useState<Assignment | null>(null);
  const [focusId, setFocusId] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const { save, isSaving } = useCatalogSave();

  const group = model.groups.find((g) => g.id === groupId) ?? model.groups[0] ?? null;
  const current = group ? currentAssignment(model, group.id) : { itemIds: [], categoryIds: [] };
  const pending = draft ?? current;
  const dirty = !sameAssignment(current, pending);

  const apply = async () => {
    if (!group || !dirty || isSaving) return;
    const outcome = await save(planAssignments(group.id, group.name, current, pending));
    if (outcome.cancelled) return;
    if (outcome.ok) {
      setDraft(null);
      onToast({ text: `Đã áp dụng ${group.name}`, tone: "success" });
    } else {
      onToast({ text: outcome.results.find((r) => r.status === "failed")?.error ?? "Không thể lưu", tone: "error" });
    }
  };

  useHotkeys(
    "mod+s",
    (event) => {
      event.preventDefault();
      void apply();
    },
    { enableOnFormTags: true },
  );

  if (!group) {
    return (
      <p className="rounded-2xl border border-dashed border-slate-300 p-10 text-center text-sm text-slate-500 dark:border-border">
        Chưa có nhóm topping nào. Tạo nhóm ở mục Nhóm Topping.
      </p>
    );
  }

  const selectGroup = (id: string) => {
    if (id === group.id) return;
    if (dirty && !window.confirm("Bỏ các thay đổi chưa lưu của nhóm này?")) return;
    setGroupId(id);
    setDraft(null);
  };
  const edit = (next: Assignment) => setDraft(next);

  const visible = filterItems(model, query, "all");
  const focusItem = model.items.find((i) => i.id === focusId) ?? visible[0] ?? null;
  const preview = focusItem ? toSellablePreview(focusItem, model, { groupId: group.id, ...pending }) : null;
  const previewKey = preview ? `${preview.id}:${(preview.modifier_groups ?? []).map((g) => g.id).join(",")}` : "none";

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-card p-4 shadow-2xs dark:border-border">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-amber-50 text-amber-600 dark:bg-amber-950/40">
          <Zap className="h-5 w-5" />
        </div>
        <div>
          <h2 className="text-sm font-bold text-slate-900 sm:text-base dark:text-foreground">Ma trận Gán Topping Hàng Loạt (Batch Linker)</h2>
          <p className="text-xs text-slate-500 dark:text-muted-foreground">
            Chọn 1 nhóm topping, tick nhanh theo danh mục hoặc nhiều món cùng lúc, và xem trước mô phỏng màn hình thu ngân tức thì.
          </p>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[28fr_42fr_30fr]">
        <section className="space-y-2 rounded-2xl border border-slate-200 bg-card p-3 dark:border-border">
          <p className="px-1 text-xs font-bold text-slate-700 dark:text-foreground">1. Nhóm Topping</p>
          {model.groups.map((g) => (
            <button
              key={g.id}
              type="button"
              onClick={() => selectGroup(g.id)}
              className={cn(
                "flex min-h-[48px] w-full items-center justify-between gap-2 rounded-xl border px-3 py-2 text-left text-xs transition",
                g.id === group.id
                  ? "border-emerald-600 bg-emerald-50 font-bold text-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-300"
                  : "border-slate-200 text-slate-700 hover:bg-slate-50 dark:border-border dark:text-foreground dark:hover:bg-muted",
              )}
            >
              <span>{g.name}</span>
              <span className="font-mono text-[10px] text-slate-400">{currentAssignment(model, g.id).itemIds.length} món</span>
            </button>
          ))}
        </section>

        <section className="flex flex-col rounded-2xl border border-slate-200 bg-card dark:border-border">
          <div className="space-y-2 border-b border-slate-100 p-3 dark:border-border">
            <p className="text-xs font-bold text-slate-700 dark:text-foreground">
              2. Danh mục & Món áp dụng · Nhóm: <span className="text-emerald-700">{group.name}</span>
            </p>
            <div className="relative">
              <Search className="pointer-events-none absolute top-1/2 left-3.5 h-4 w-4 -translate-y-1/2 text-slate-400" />
              <input
                aria-label="Tìm món"
                className={`${INPUT_CLASS} pl-10`}
                value={query}
                placeholder="Tìm món theo tên hoặc viết tắt (vd: cfsd, tdcs)..."
                onChange={(e) => setQuery(e.target.value)}
              />
            </div>
          </div>
          <div className="max-h-[60vh] flex-1 space-y-3 overflow-y-auto p-3">
            {model.categories.map((c) => {
              const items = visible.filter((i) => i.categoryId === c.id);
              if (items.length === 0 && query) return null;
              return (
                <div key={c.id} className="space-y-1.5">
                  <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-slate-50 px-2 py-1 dark:bg-muted/40">
                    <label className="flex min-h-[48px] cursor-pointer items-center gap-2 text-xs font-bold text-slate-800 dark:text-foreground">
                      <input
                        type="checkbox"
                        className="h-5 w-5 accent-emerald-600"
                        checked={pending.categoryIds.includes(c.id)}
                        onChange={() => edit(toggleCategory(pending, c.id))}
                      />
                      {c.name} · Gán cho cả danh mục
                    </label>
                    <span className="text-[11px] text-slate-500">
                      <button type="button" className="min-h-[48px] px-1 font-semibold text-emerald-700" onClick={() => edit(selectAllInCategory(pending, model, group.id, c.id))}>
                        Chọn hết
                      </button>
                      |
                      <button type="button" className="min-h-[48px] px-1 font-semibold text-slate-500" onClick={() => edit(clearCategoryItems(pending, model, c.id))}>
                        Bỏ chọn hết
                      </button>
                    </span>
                  </div>
                  {items.map((i) => {
                    const state = linkState(i, group.id, pending);
                    return (
                      <label
                        key={i.id}
                        onMouseEnter={() => setFocusId(i.id)}
                        className={cn(
                          "flex min-h-[48px] cursor-pointer items-center gap-3 rounded-xl border px-3 text-xs",
                          focusItem?.id === i.id ? "border-emerald-300" : "border-transparent",
                          state === "excluded" && "cursor-not-allowed opacity-60",
                        )}
                      >
                        <input
                          type="checkbox"
                          className="h-5 w-5 accent-emerald-600"
                          disabled={state === "excluded"}
                          checked={state === "direct"}
                          onChange={() => {
                            setFocusId(i.id);
                            edit(toggleItem(pending, i.id));
                          }}
                        />
                        <span className="flex-1 font-semibold text-slate-800 dark:text-foreground">{i.name}</span>
                        {STATE_LABEL[state] && <span className="text-[10px] font-semibold text-slate-500">{STATE_LABEL[state]}</span>}
                      </label>
                    );
                  })}
                </div>
              );
            })}
          </div>
          <div className="sticky bottom-0 flex flex-wrap items-center justify-between gap-3 border-t border-slate-100 bg-card p-3 dark:border-border">
            <span className="text-xs text-slate-500">
              Đã chọn: <span className="font-mono font-bold text-slate-900 dark:text-foreground">{pending.itemIds.length}</span> món cho nhóm{" "}
              {group.name}
            </span>
            <button
              type="button"
              disabled={!dirty || isSaving}
              onClick={() => void apply()}
              className="flex h-14 min-h-[48px] items-center gap-2 rounded-xl bg-emerald-600 px-6 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 active:scale-[0.98] disabled:opacity-50 sm:text-sm"
            >
              {isSaving ? "Đang lưu…" : "LƯU ÁP DỤNG NGAY (Ctrl+S)"}
            </button>
          </div>
        </section>

        <section className="flex flex-col overflow-hidden rounded-2xl border border-slate-200 bg-card dark:border-border">
          <div className="flex items-center gap-2 border-b border-slate-100 p-3 dark:border-border">
            <MonitorSmartphone className="h-4 w-4 text-emerald-600" />
            <p className="text-xs font-bold text-slate-700 dark:text-foreground">Mô phỏng màn hình thu ngân</p>
            <span className="ml-auto rounded-full bg-emerald-50 px-2 py-0.5 text-[10px] font-bold text-emerald-700">Live POS Sync</span>
          </div>
          {preview ? (
            <div className="flex max-h-[70vh] flex-col">
              <p className="px-4 pt-3 text-sm font-bold text-slate-900 dark:text-foreground">{preview.name}</p>
              <ItemPickerBody key={previewKey} item={preview} onConfirm={() => {}} isSubmitting confirmLabel="Chỉ xem trước" />
            </div>
          ) : (
            <p className="p-6 text-center text-xs text-slate-400">Chọn một món để xem trước.</p>
          )}
        </section>
      </div>
    </div>
  );
}
```

- [ ] **Step 6: Type-check and run tests**

Run: `cd web && bun test src/features/catalog && bunx tsc -b`
Expected: PASS; tsc exit 0.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/catalog/lib/linker.ts web/src/features/catalog/lib/linker.test.ts web/src/features/catalog/components/linker-view.tsx
git commit -m "feat(web): batch linker with live POS preview"
```

---

### Task 15: Tab shell, route, and settings wiring

**Files:**
- Create: `web/src/features/catalog/lib/views.ts`
- Create: `web/src/features/catalog/components/catalog-nav.tsx`
- Create: `web/src/features/catalog/components/catalog-view.tsx`
- Create: `web/src/routes/_app/settings/catalog.tsx`
- Modify: `web/src/features/settings/lib/tabs.ts`
- Modify: `web/src/features/settings/lib/tabs.test.ts`
- Modify: `web/src/routes/_app/settings.tsx`
- Regenerate: `web/src/routeTree.gen.ts`
- Test: `web/src/features/catalog/lib/views.test.ts`
- Test: `web/src/features/catalog/components/catalog-nav.test.tsx`

**Interfaces:**
- Consumes: every earlier task; `StockToast` from `@/features/settings/components/stock-toast`; `requireCapability` from `@/lib/guards`.
- Produces: `CATALOG_VIEWS`, `CatalogViewKey`, `parseCatalogView(value)`; `CatalogHeader({ canAdd, onNewItem })`, `CatalogPills({ view, counts, onChange })`; `CatalogView({ view, onViewChange })`; the route `/settings/catalog?view=…`.

- [ ] **Step 1: Write the failing tests**

```ts
// web/src/features/catalog/lib/views.test.ts
import { describe, expect, it } from "bun:test";
import { parseCatalogView } from "./views";

describe("parseCatalogView", () => {
  it("accepts the four views and falls back to items", () => {
    for (const v of ["items", "categories", "groups", "linker"]) expect(parseCatalogView(v)).toBe(v);
    expect(parseCatalogView("nope")).toBe("items");
    expect(parseCatalogView(undefined)).toBe("items");
  });
});
```

```tsx
// web/src/features/catalog/components/catalog-nav.test.tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { CatalogHeader, CatalogPills } from "./catalog-nav";

describe("catalog nav", () => {
  it("renders the four pills with counts and marks the active one", () => {
    const html = renderToString(
      <CatalogPills view="groups" counts={{ items: 12, categories: 5, groups: 4 }} onChange={() => {}} />,
    );
    // renderToString escapes "&" as "&amp;".
    for (const label of ["Món &amp; Định giá", "Danh mục món", "Nhóm Topping", "Ma trận Gán Topping (Batch Linker)"]) {
      expect(html).toContain(label);
    }
    expect(html).toContain("12 món");
    expect(html).toContain("5 danh mục");
    expect(html).toContain("4 nhóm");
    expect(html).toMatch(/aria-selected="true"[^>]*>[\s\S]*?Nhóm Topping/);
  });

  it("renders the hero with the add button", () => {
    const html = renderToString(<CatalogHeader canAdd onNewItem={() => {}} />);
    expect(html).toContain("Trung tâm Quản lý Thực đơn &amp; Nhóm Topping");
    expect(html).toContain("+ THÊM MÓN MỚI (Ctrl+N)");
  });
});
```

Update `web/src/features/settings/lib/tabs.test.ts`. Replace the `describe("settings tabs", …)` and `describe("planned tabs", …)` blocks with:

```ts
describe("settings tabs", () => {
  it("lists availability for anyone who manages availability", () => {
    expect(firstPermittedTab(["catalog.manage_availability"])?.to).toBe("/settings/availability");
    expect(permittedTabs(["catalog.manage_availability"]).map((t) => t.label)).toEqual(["Kho & Món Tạm Hết"]);
  });

  it("gives a manager both routable tabs", () => {
    expect(permittedTabs(MANAGER).map((t) => t.to)).toEqual(["/settings/availability", "/settings/catalog"]);
  });

  it("has no tab for a session without a settings capability", () => {
    expect(firstPermittedTab(["sales.operate"])).toBeNull();
  });

  it("exposes every routable tab capability for the layout guard", () => {
    expect(SETTINGS_CAPABILITIES).toEqual(["catalog.manage_availability", "catalog.administer_structure"]);
  });
});

describe("planned tabs", () => {
  it("shows a manager the two tabs still to come", () => {
    expect(visiblePlannedTabs(MANAGER).map((t) => t.label)).toEqual(["Thông tin Quán & VietQR", "Tùy chỉnh In & Hệ thống"]);
  });

  it("shows a barista or cashier none of them", () => {
    expect(visiblePlannedTabs(["catalog.manage_availability", "preparation.operate"])).toEqual([]);
    expect(visiblePlannedTabs(["catalog.manage_availability", "sales.operate", "sales_shift.operate"])).toEqual([]);
  });

  it("never grants access to the settings layout", () => {
    expect(SETTINGS_CAPABILITIES).not.toContain("staff.administer");
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun test src/features/catalog/lib/views.test.ts src/features/catalog/components/catalog-nav.test.tsx src/features/settings/lib/tabs.test.ts`
Expected: FAIL. Modules are not found, and the tabs expectations do not match.

- [ ] **Step 3: Write `views.ts`**

```ts
// web/src/features/catalog/lib/views.ts
export const CATALOG_VIEWS = ["items", "categories", "groups", "linker"] as const;

export type CatalogViewKey = (typeof CATALOG_VIEWS)[number];

export function parseCatalogView(value: unknown): CatalogViewKey {
  return CATALOG_VIEWS.includes(value as CatalogViewKey) ? (value as CatalogViewKey) : "items";
}
```

- [ ] **Step 4: Write `catalog-nav.tsx`**

```tsx
// web/src/features/catalog/components/catalog-nav.tsx
import type { ReactElement } from "react";
import { Coffee, Layers, LayoutGrid, Plus, Utensils, Zap, type LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import type { CatalogViewKey } from "../lib/views";

export interface CatalogCounts {
  items: number;
  categories: number;
  groups: number;
}

const PILLS: { view: CatalogViewKey; label: string; icon: LucideIcon; count?: (c: CatalogCounts) => string }[] = [
  { view: "items", label: "Món & Định giá", icon: Coffee, count: (c) => `${c.items} món` },
  { view: "categories", label: "Danh mục món", icon: LayoutGrid, count: (c) => `${c.categories} danh mục` },
  { view: "groups", label: "Nhóm Topping", icon: Layers, count: (c) => `${c.groups} nhóm` },
  { view: "linker", label: "Ma trận Gán Topping (Batch Linker)", icon: Zap },
];

export function CatalogHeader({ canAdd, onNewItem }: { canAdd: boolean; onNewItem: () => void }): ReactElement {
  return (
    <div className="flex flex-col justify-between gap-4 rounded-2xl border border-slate-200 bg-card p-5 shadow-2xs sm:flex-row sm:items-center dark:border-border">
      <div className="flex items-center gap-3.5">
        <div className="flex h-12 w-12 items-center justify-center rounded-2xl border border-emerald-200 bg-emerald-50 text-emerald-600 dark:border-emerald-900 dark:bg-emerald-950/40">
          <Utensils className="h-6 w-6" />
        </div>
        <div>
          <h2 className="text-base leading-tight font-bold text-slate-900 sm:text-lg dark:text-foreground">
            Trung tâm Quản lý Thực đơn & Nhóm Topping
          </h2>
          <p className="mt-1 text-xs text-slate-500 dark:text-muted-foreground">
            Thiết lập món, định giá đa kích cỡ (S/M/L), nhóm tùy chọn và ma trận gán topping hàng loạt cho quầy thu ngân.
          </p>
        </div>
      </div>
      <button
        type="button"
        disabled={!canAdd}
        title={canAdd ? undefined : "Tạo danh mục trước"}
        onClick={onNewItem}
        className="flex h-12 min-h-[48px] shrink-0 items-center justify-center gap-2 rounded-xl bg-emerald-600 px-5 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 active:scale-[0.98] disabled:opacity-50 sm:text-sm"
      >
        <Plus className="h-4 w-4" />+ THÊM MÓN MỚI (Ctrl+N)
      </button>
    </div>
  );
}

export function CatalogPills({
  view,
  counts,
  onChange,
}: {
  view: CatalogViewKey;
  counts: CatalogCounts;
  onChange: (view: CatalogViewKey) => void;
}): ReactElement {
  return (
    <div className="rounded-2xl border border-slate-200 bg-card p-2 shadow-2xs dark:border-border">
      <div role="tablist" aria-label="Phân hệ thực đơn" className="flex items-center gap-2 overflow-x-auto">
        {PILLS.map(({ view: key, label, icon: Icon, count }) => {
          const active = key === view;
          return (
            <button
              key={key}
              type="button"
              role="tab"
              aria-selected={active}
              onClick={() => onChange(key)}
              className={cn(
                "flex h-12 min-h-[48px] shrink-0 items-center gap-2 rounded-xl px-4 text-xs whitespace-nowrap transition select-none sm:text-sm",
                active
                  ? "bg-slate-900 font-bold text-white shadow-xs dark:bg-foreground dark:text-background"
                  : "font-semibold text-slate-600 hover:bg-slate-100 dark:text-muted-foreground dark:hover:bg-muted",
              )}
            >
              <Icon className={cn("h-4 w-4", active ? "text-white dark:text-background" : "text-slate-400")} />
              <span>{label}</span>
              {count && (
                <span
                  className={cn(
                    "rounded-full px-2 py-0.5 font-mono text-[11px] font-bold",
                    active ? "bg-white/15 text-white" : "bg-slate-100 text-slate-600 dark:bg-muted",
                  )}
                >
                  {count(counts)}
                </span>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
```

- [ ] **Step 5: Write `catalog-view.tsx`**

```tsx
// web/src/features/catalog/components/catalog-view.tsx
import { useCallback, useRef, useState, type ReactElement } from "react";
import { useHotkeys } from "react-hotkeys-hook";
import { Button } from "@/components/ui/button";
import { StockToast, type StockToastMessage } from "@/features/settings/components/stock-toast";
import { messageForError } from "@/lib/error-messages";
import { useCatalogModel } from "../api/use-catalog-admin";
import type { CatalogViewKey } from "../lib/views";
import { CategoriesView } from "./categories-view";
import { CatalogHeader, CatalogPills } from "./catalog-nav";
import { GroupsView } from "./groups-view";
import { ItemFormModal } from "./item-form-modal";
import { ItemsView } from "./items-view";
import { LinkerView } from "./linker-view";

export interface CatalogViewProps {
  view: CatalogViewKey;
  onViewChange: (view: CatalogViewKey) => void;
}

/** The "Quản lý Thực đơn & Topping" tab. It hosts the item modal so the hero button and Ctrl+N reach it from any view. */
export function CatalogView({ view, onViewChange }: CatalogViewProps): ReactElement {
  const { model, isPending, error, refetch } = useCatalogModel();
  const [toast, setToast] = useState<StockToastMessage | null>(null);
  const [itemEditor, setItemEditor] = useState<{ id: string | null; session: number } | null>(null);
  const sessions = useRef(0);
  const dismissToast = useCallback(() => setToast(null), []);
  const openItem = useCallback((id: string | null) => setItemEditor({ id, session: ++sessions.current }), []);
  const canAdd = model.categories.length > 0;
  const newItem = () => {
    onViewChange("items");
    openItem(null);
  };

  useHotkeys(
    "mod+n",
    (event) => {
      event.preventDefault();
      if (canAdd) newItem();
    },
    { enabled: itemEditor === null && !isPending },
  );

  if (isPending) {
    return (
      <div className="space-y-5">
        <div className="h-[92px] animate-pulse rounded-2xl bg-slate-200/60 dark:bg-muted" />
        <div className="h-16 animate-pulse rounded-2xl bg-slate-200/60 dark:bg-muted" />
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 6 }, (_, i) => (
            <div key={i} className="h-[114px] animate-pulse rounded-2xl bg-slate-200/60 dark:bg-muted" />
          ))}
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center gap-3 rounded-2xl border border-slate-200 bg-card p-12 text-center dark:border-border">
        <p role="alert" className="text-sm font-semibold text-destructive">
          {messageForError(error)}
        </p>
        <Button onClick={refetch} className="h-12 min-h-[48px] rounded-xl px-6">
          Thử lại
        </Button>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <CatalogHeader canAdd={canAdd} onNewItem={newItem} />
      <CatalogPills
        view={view}
        counts={{ items: model.items.length, categories: model.categories.length, groups: model.groups.length }}
        onChange={onViewChange}
      />
      {view === "items" && <ItemsView model={model} onOpenItem={openItem} />}
      {view === "categories" && <CategoriesView model={model} onToast={setToast} />}
      {view === "groups" && <GroupsView model={model} onToast={setToast} />}
      {view === "linker" && <LinkerView model={model} onToast={setToast} />}
      {itemEditor && (
        <ItemFormModal
          key={itemEditor.session}
          snapshot={model.items.find((i) => i.id === itemEditor.id) ?? null}
          model={model}
          defaultCategoryId={model.categories[0]?.id ?? ""}
          onClose={() => setItemEditor(null)}
          onCreated={(id) => setItemEditor((e) => (e ? { ...e, id } : e))}
          onSaved={(text) => setToast({ text, tone: "success" })}
        />
      )}
      <StockToast toast={toast} onDismiss={dismissToast} />
    </div>
  );
}
```

- [ ] **Step 6: Write the route**

```tsx
// web/src/routes/_app/settings/catalog.tsx
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { CatalogView } from "@/features/catalog/components/catalog-view";
import { parseCatalogView, type CatalogViewKey } from "@/features/catalog/lib/views";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/settings/catalog")({
  validateSearch: (search: Record<string, unknown>): { view?: CatalogViewKey } => ({
    view: parseCatalogView(search.view),
  }),
  beforeLoad: () => requireCapability("catalog.administer_structure"),
  component: CatalogRoute,
});

function CatalogRoute() {
  const { view = "items" } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  return <CatalogView view={view} onViewChange={(next) => void navigate({ search: { view: next } })} />;
}
```

- [ ] **Step 7: Update `tabs.ts`**

In `web/src/features/settings/lib/tabs.ts`:

1. Change `SettingsTab.to` to `to: "/settings/availability" | "/settings/catalog";`.
2. Append to `SETTINGS_TABS`:

```ts
  {
    to: "/settings/catalog",
    label: "Quản lý Thực đơn & Topping",
    icon: Utensils,
    capability: "catalog.administer_structure",
  },
```

3. In `PlannedSettingsTab`, change `key` to `key: "store" | "system";`, and delete the `catalog` entry from `PLANNED_SETTINGS_TABS`.
4. Update the planned-tabs doc comment: "Tabs the design shows that later work builds (store and print settings, which have no endpoint yet)."

- [ ] **Step 8: Rekey the tab counter**

In `web/src/routes/_app/settings.tsx`, change the counters object to:

```tsx
<SettingsLayout counters={{ "/settings/availability": <AvailabilityCounter />, "/settings/catalog": <CatalogItemCounter /> }} />
```

- [ ] **Step 9: Regenerate the route tree and verify**

Run: `cd web && bunx vite build; git checkout -- dist/.gitkeep`
Expected: build succeeds, and `src/routeTree.gen.ts` now contains `/_app/settings/catalog`.

Run: `cd web && bun test && bunx tsc -b && bun run lint`
Expected: all tests pass; tsc and lint exit 0.

- [ ] **Step 10: Commit**

```bash
git add web/src/features/catalog/lib/views.ts web/src/features/catalog/lib/views.test.ts web/src/features/catalog/components/catalog-nav.tsx web/src/features/catalog/components/catalog-nav.test.tsx web/src/features/catalog/components/catalog-view.tsx web/src/routes/_app/settings/catalog.tsx web/src/routes/_app/settings.tsx web/src/features/settings/lib/tabs.ts web/src/features/settings/lib/tabs.test.ts web/src/routeTree.gen.ts
git commit -m "feat(web): catalog structure tab under settings"
```

---

### Task 16: ADR-062, full verification, UAT hand-off

**Files:**
- Modify: `spec/decisions.md` (append ADR-062)

- [ ] **Step 1: Append ADR-062**

Append to `spec/decisions.md`, after ADR-061:

```markdown

## ADR-062: A web form save is a planned sequence of existing commands

* **Decision Date:** 2026-09-30
* **Status:** Accepted
* **Context:** The slice 9b item, category, and modifier-group forms each edit fields that belong to several catalog commands (rename, reprice, sizes, details, image, assignments), and the prototype gives each form one "Lưu" button. The commands are separate by design: each carries its own capability, Manager PIN rule, and Audit Event (ADR-048).
* **Decision:**
* "Lưu" diffs the form against the snapshot taken when the modal opened and runs the resulting commands in a fixed order that never exposes an invalid intermediate state (move before assignments; add options before the selection rule; the rule before retiring options). The Manager PIN is asked once when any command needs it and is held only for that run.
* The run stops at the first failure and shows which steps finished. Pressing "Lưu" again re-plans against refreshed server state instead of replaying request ids: finished steps no longer differ and drop out, unsaved rows are matched to rows the server already created by name, and a finished image upload is removed from the form.
* **Rejected:** a composite backend command per form, which would merge capabilities and Audit Events that ADR-048 keeps one per intent; one save button per form section, which departs from the prototype's single save.
* **Consequences:** A save can stop part-way, and the form says so. A concurrent edit of the same entity on another terminal can be overwritten, because details and replace-set commands replace whole values. This is accepted while one Manager edits the menu.
```

- [ ] **Step 2: Full verification**

Run from the repo root: `go build ./... && go vet ./...`. There are no Go changes, so this only confirms nothing broke.

Then run: `cd web && bun test && bunx tsc -b && bun run lint && bun run build; git checkout -- dist/.gitkeep`
Expected: every command exits 0, and `git status` shows no deleted `web/dist/.gitkeep`.

- [ ] **Step 3: Commit**

```bash
git add spec/decisions.md
git commit -m "docs: ADR-062, planned multi-command form saves"
```

- [ ] **Step 4: Hand over the UAT gate and stop**

Setup: `make docker-up`, run the API, `make dev-seed`, `cd web && bun run dev`. Sign in as Manager and open "Cài đặt", then "Quản lý Thực đơn & Topping".

Hand the operator the spec's UAT list (§10) verbatim:

1. Create an item with two sizes, an image, a code, a badge, and a modifier group. It appears on the POS (F1), and its picker shows the sizes and group.
2. Edit it: rename, change one size's price (the PIN is asked **once**), add a size, retire a size. The POS reflects it; an earlier Completed Sale keeps its old price.
3. Move an item that excludes a group to a category that does not provide the group. The exclusion disappears after save.
4. Go offline in DevTools after the first step of a multi-step save. The progress list shows ✓ and ✗. Go online, press "Lưu" again: only the remaining steps run, and the audit log has no duplicate event.
5. Create a group with three options using Enter; set min 1, max 2, and a default. Selecting three defaults is blocked on the form.
6. In the linker, assign a group to category Trà and untick one item. The preview updates live; after saving, the POS matches. An item excluding the group is disabled.
7. Create a category with an icon and order; retire it with reason "Khác" and no note: blocked until a note is entered.
8. As Cashier and as Barista, the tab is absent, and opening `/settings/catalog` directly is refused by the guard.

Implementation stops here. Do not mark 9b done in `ROADMAP.md` until the operator confirms UAT.
```
