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

export interface DraftItemConfigLike {
  sizeId?: string;
  selectedOptionIds: string[];
  preparationNote: string;
  quantity?: number;
}

/**
 * Checks whether a draft line item matches a desired item picker configuration.
 */
export function matchesDraftItemConfig(
  draftItem: {
    menu_item_id?: string;
    size_id?: string;
    preparation_note?: string;
    selected_modifier_options?: Array<{ id?: string }>;
  },
  menuItemId: string,
  config: DraftItemConfigLike,
): boolean {
  if (draftItem.menu_item_id !== menuItemId) return false;
  const itemSizeId = draftItem.size_id || undefined;
  const configSizeId = config.sizeId || undefined;
  if (itemSizeId !== configSizeId) return false;

  const itemNote = draftItem.preparation_note || "";
  const configNote = config.preparationNote || "";
  if (itemNote !== configNote) return false;

  const itemOptionIds = (draftItem.selected_modifier_options ?? [])
    .map((opt) => opt.id)
    .filter(Boolean)
    .sort()
    .join(",");
  const configOptionIds = [...config.selectedOptionIds].sort().join(",");

  return itemOptionIds === configOptionIds;
}

export interface DraftItemEditsDiff {
  quantityChanged: boolean;
  modifiersChanged: boolean;
  noteChanged: boolean;
  sizeChanged: boolean;
}

/**
 * Diffs current picker configuration against initial values to only update modified attributes.
 */
export function diffDraftItemEdits(
  initialValues: Partial<DraftItemConfigLike> | undefined,
  config: DraftItemConfigLike,
): DraftItemEditsDiff {
  const quantityChanged =
    initialValues?.quantity !== undefined &&
    config.quantity !== undefined &&
    config.quantity !== initialValues.quantity;

  const prevModIds = [...(initialValues?.selectedOptionIds ?? [])]
    .sort()
    .join(",");
  const nextModIds = [...config.selectedOptionIds].sort().join(",");
  const modifiersChanged = prevModIds !== nextModIds;

  const prevNote = initialValues?.preparationNote ?? "";
  const nextNote = config.preparationNote ?? "";
  const noteChanged = prevNote !== nextNote;

  const sizeChanged = Boolean(
    config.sizeId && config.sizeId !== initialValues?.sizeId,
  );

  return {
    quantityChanged,
    modifiersChanged,
    noteChanged,
    sizeChanged,
  };
}

