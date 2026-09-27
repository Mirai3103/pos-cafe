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
