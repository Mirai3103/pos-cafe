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
