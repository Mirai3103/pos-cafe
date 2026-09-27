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
