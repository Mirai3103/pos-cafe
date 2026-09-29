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
