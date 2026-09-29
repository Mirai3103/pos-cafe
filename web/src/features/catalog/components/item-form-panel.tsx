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
