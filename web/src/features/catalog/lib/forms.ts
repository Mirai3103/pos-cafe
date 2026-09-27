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
