import * as React from "react";
import type {
  CatalogSellableCategoryResponse,
  CatalogSellableItemResponse,
  SalesDraftItemResponse,
  SalesServiceSessionResponse,
} from "@/api/generated/models";
import { messageForError } from "@/lib/error-messages";
import {
  useAddDraftItem,
  useRemoveDraftItem,
  useUpdateDraftItemModifiers,
  useUpdateDraftItemPreparationNote,
  useUpdateDraftItemQuantity,
  useUpdateDraftItemSize,
} from "../api/use-pos";
import type { ItemPickerConfig, ItemPickerDialogProps } from "../components/item-picker-dialog";
import { diffDraftItemEdits, matchesDraftItemConfig } from "../lib/selection";

export interface DraftEditorOptions {
  activeSessionId: string | null;
  session: SalesServiceSessionResponse | null;
  /** The sellable menu, used to find the catalog item behind a draft line. */
  categories: CatalogSellableCategoryResponse[] | undefined;
  isShiftOpen: boolean;
  ensureSessionId: () => Promise<string>;
  ensureDraft: (sessionId: string) => Promise<void>;
  /** Sets (or, with null, clears) the page-level error toast. */
  setErrorMessage: (message: string | null) => void;
}

export interface DraftEditor {
  /** Props for the item configuration dialog. */
  pickerProps: ItemPickerDialogProps;
  isPickerOpen: boolean;
  /** A menu tap: adds a simple item directly, or opens the picker to configure it. */
  selectItem: (item: CatalogSellableItemResponse) => Promise<void>;
  /** Reopens the picker on an existing draft line. */
  editDraftItem: (draftItem: SalesDraftItemResponse) => void;
  changeQuantity: (itemId: string, nextQty: number) => Promise<void>;
  removeItem: (itemId: string) => Promise<void>;
}

function findCatalogItem(
  categories: CatalogSellableCategoryResponse[] | undefined,
  menuItemId: string | undefined,
): CatalogSellableItemResponse | null {
  for (const cat of categories ?? []) {
    for (const it of cat.items ?? []) {
      if (it.id === menuItemId) return it;
    }
  }
  return null;
}

/**
 * Edits the Order Draft: the item picker (adding a configured item or
 * changing an existing line) and the inline quantity/remove controls.
 */
export function useDraftEditor({
  activeSessionId,
  session,
  categories,
  isShiftOpen,
  ensureSessionId,
  ensureDraft,
  setErrorMessage,
}: DraftEditorOptions): DraftEditor {
  const { addDraftItem, isPending: isAddingItem } = useAddDraftItem(activeSessionId ?? "");
  const { updateQuantity } = useUpdateDraftItemQuantity(activeSessionId ?? "");
  const { updateSize } = useUpdateDraftItemSize(activeSessionId ?? "");
  const { updateModifiers } = useUpdateDraftItemModifiers(activeSessionId ?? "");
  const { updatePreparationNote } = useUpdateDraftItemPreparationNote(activeSessionId ?? "");
  const { removeDraftItem } = useRemoveDraftItem(activeSessionId ?? "");

  const [pickerItem, setPickerItem] = React.useState<CatalogSellableItemResponse | null>(null);
  const [editingDraftItemId, setEditingDraftItemId] = React.useState<string | null>(null);
  const [pickerInitialValues, setPickerInitialValues] = React.useState<
    Partial<ItemPickerConfig> | undefined
  >(undefined);
  const [isPickerOpen, setIsPickerOpen] = React.useState(false);
  const [isSubmittingPicker, setIsSubmittingPicker] = React.useState(false);

  const selectItem = async (item: CatalogSellableItemResponse) => {
    if (!isShiftOpen || !item.id) return;
    setErrorMessage(null);

    const hasSizes = Boolean(item.sizes && item.sizes.length > 0);
    const hasModifiers = Boolean(item.modifier_groups && item.modifier_groups.length > 0);

    // Simple item: 1-tap direct add
    if (!hasSizes && !hasModifiers) {
      try {
        const sid = await ensureSessionId();
        await ensureDraft(sid);
        await addDraftItem({ menu_item_id: item.id }, undefined, sid);
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

  const editDraftItem = (draftItem: SalesDraftItemResponse) => {
    if (!isShiftOpen) return;
    setErrorMessage(null);

    const catalogItem = findCatalogItem(categories, draftItem.menu_item_id);
    if (!catalogItem) return;

    setPickerItem(catalogItem);
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

  /** Applies only the attributes that changed on an existing draft line. */
  const applyEdits = async (draftItemId: string, config: ItemPickerConfig) => {
    const diff = diffDraftItemEdits(pickerInitialValues, config);

    if (diff.quantityChanged && config.quantity !== undefined) {
      await updateQuantity(draftItemId, { quantity: config.quantity });
    }
    if (diff.modifiersChanged) {
      await updateModifiers(draftItemId, { modifier_option_ids: config.selectedOptionIds });
    }
    if (diff.noteChanged) {
      await updatePreparationNote(draftItemId, { preparation_note: config.preparationNote });
    }
    // Size goes last to avoid the backend merge-deletion race.
    if (diff.sizeChanged && config.sizeId) {
      await updateSize(draftItemId, { size_id: config.sizeId });
    }
  };

  const addConfiguredItem = async (menuItemId: string, config: ItemPickerConfig) => {
    const prevItem = session?.draft?.items?.find((it) =>
      matchesDraftItemConfig(it, menuItemId, config),
    );
    const sid = await ensureSessionId();
    await ensureDraft(sid);
    const updatedSession = await addDraftItem(
      {
        menu_item_id: menuItemId,
        size_id: config.sizeId,
        modifier_option_ids: config.selectedOptionIds,
        preparation_note: config.preparationNote || undefined,
      },
      undefined,
      sid,
    );

    // Preflight ruling: if quantity > 1 on add, update quantity so user choice is preserved
    if (config.quantity > 1 && updatedSession?.draft?.items) {
      const targetItem =
        updatedSession.draft.items
          .slice()
          .reverse()
          .find((it) => matchesDraftItemConfig(it, menuItemId, config)) ??
        updatedSession.draft.items.slice(-1)[0];

      if (targetItem?.id) {
        const targetQty = (prevItem?.quantity ?? 0) + config.quantity;
        await updateQuantity(targetItem.id, { quantity: targetQty }, undefined, sid);
      }
    }
  };

  const confirmPicker = async (config: ItemPickerConfig) => {
    if (!pickerItem?.id) return;
    setErrorMessage(null);
    setIsSubmittingPicker(true);

    try {
      if (editingDraftItemId) {
        await applyEdits(editingDraftItemId, config);
      } else {
        await addConfiguredItem(pickerItem.id, config);
      }
      setIsPickerOpen(false);
    } catch (err) {
      setErrorMessage(messageForError(err));
    } finally {
      setIsSubmittingPicker(false);
    }
  };

  const changeQuantity = async (itemId: string, nextQty: number) => {
    setErrorMessage(null);
    try {
      await updateQuantity(itemId, { quantity: nextQty });
    } catch (err) {
      setErrorMessage(messageForError(err));
    }
  };

  const removeItem = async (itemId: string) => {
    setErrorMessage(null);
    try {
      await removeDraftItem(itemId);
    } catch (err) {
      setErrorMessage(messageForError(err));
    }
  };

  return {
    pickerProps: {
      item: pickerItem,
      initialValues: pickerInitialValues,
      isOpen: isPickerOpen,
      onClose: () => setIsPickerOpen(false),
      onConfirm: confirmPicker,
      isSubmitting: isAddingItem || isSubmittingPicker,
      confirmLabel: editingDraftItemId ? "Cập nhật món" : "Thêm vào đơn",
    },
    isPickerOpen,
    selectItem,
    editDraftItem,
    changeQuantity,
    removeItem,
  };
}
