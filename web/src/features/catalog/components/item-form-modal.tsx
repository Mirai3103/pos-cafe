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
