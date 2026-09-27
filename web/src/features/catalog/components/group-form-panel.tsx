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
