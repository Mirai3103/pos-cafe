// web/src/features/catalog/components/categories-view.tsx
import { useRef, useState, type ReactElement } from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";
import type { StockToastMessage } from "@/features/settings/components/stock-toast";
import { useRetireEntity } from "../api/use-catalog-admin";
import type { CatalogModel, CatCategory } from "../lib/catalog-model";
import { categoryIcon } from "../lib/category-icons";
import { CategoryFormModal } from "./category-form-modal";
import { RetireDialog } from "./retire-dialog";

export interface CategoriesViewProps {
  model: CatalogModel;
  onToast: (toast: StockToastMessage) => void;
}

export function CategoriesView({ model, onToast }: CategoriesViewProps): ReactElement {
  const [editing, setEditing] = useState<{ id: string | null; session: number } | null>(null);
  const [retiring, setRetiring] = useState<CatCategory | null>(null);
  const sessions = useRef(0);
  const retireEntity = useRetireEntity();
  const groupName = new Map(model.groups.map((g) => [g.id, g.name]));
  const open = (id: string | null) => setEditing({ id, session: ++sessions.current });

  return (
    <div className="space-y-5">
      <div className="flex flex-col justify-between gap-3 rounded-2xl border border-slate-200 bg-card p-4 shadow-2xs sm:flex-row sm:items-center dark:border-border">
        <div>
          <h2 className="text-sm font-bold text-slate-900 sm:text-base dark:text-foreground">Danh mục Món ăn & Đồ uống</h2>
          <p className="text-xs text-slate-500 dark:text-muted-foreground">
            Quản lý nhóm phân loại thực đơn, thứ tự hiển thị trên quầy thu ngân và cấu hình nhóm tùy chọn kế thừa mặc định.
          </p>
        </div>
        <button
          type="button"
          onClick={() => open(null)}
          className="flex h-12 min-h-[48px] shrink-0 items-center justify-center gap-2 rounded-xl bg-emerald-600 px-5 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 active:scale-[0.98] sm:text-sm"
        >
          <Plus className="h-4 w-4" />+ THÊM DANH MỤC
        </button>
      </div>

      <p className="text-xs text-slate-500 dark:text-muted-foreground">
        Hiển thị: <span className="font-mono font-bold text-slate-900 dark:text-foreground">{model.categories.length} danh mục</span>
      </p>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {model.categories.map((c) => {
          const Icon = categoryIcon(c.icon);
          const count = model.items.filter((i) => i.categoryId === c.id).length;
          return (
            <div key={c.id} className="flex flex-col gap-3 rounded-2xl border border-slate-200 bg-card p-4 shadow-2xs dark:border-border">
              <div className="flex items-start justify-between gap-3">
                <div className="flex items-center gap-3">
                  <div className="flex h-11 w-11 items-center justify-center rounded-xl border border-emerald-200 bg-emerald-50 text-emerald-600 dark:border-emerald-900 dark:bg-emerald-950/40">
                    <Icon className="h-5 w-5" />
                  </div>
                  <div>
                    <p className="text-sm font-bold text-slate-900 dark:text-foreground">{c.name}</p>
                    <p className="text-[11px] text-slate-500">
                      Thứ tự {c.displayOrder} · {count} món
                    </p>
                  </div>
                </div>
                <div className="flex gap-1.5">
                  <button
                    type="button"
                    aria-label={`Sửa ${c.name}`}
                    onClick={() => open(c.id)}
                    className="flex h-12 min-h-[48px] w-12 min-w-[48px] items-center justify-center rounded-xl border border-slate-200 text-slate-600 hover:bg-slate-50 dark:border-border"
                  >
                    <Pencil className="h-4 w-4" />
                  </button>
                  <button
                    type="button"
                    aria-label={`Ngừng bán ${c.name}`}
                    onClick={() => setRetiring(c)}
                    className="flex h-12 min-h-[48px] w-12 min-w-[48px] items-center justify-center rounded-xl border border-rose-200 bg-rose-50 text-rose-700 hover:bg-rose-100 dark:border-rose-900 dark:bg-rose-950/30"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </div>
              <div className="flex flex-wrap gap-1.5">
                {c.groupIds.length === 0 ? (
                  <span className="text-[11px] text-slate-400">Chưa có nhóm mặc định</span>
                ) : (
                  c.groupIds.map((id) => (
                    <span key={id} className="rounded-lg bg-slate-100 px-2 py-1 text-[11px] font-semibold text-slate-600 dark:bg-muted dark:text-muted-foreground">
                      {groupName.get(id)}
                    </span>
                  ))
                )}
              </div>
            </div>
          );
        })}
      </div>

      {editing && (
        <CategoryFormModal
          key={editing.session}
          snapshot={model.categories.find((c) => c.id === editing.id) ?? null}
          model={model}
          onClose={() => setEditing(null)}
          onCreated={(id) => setEditing((e) => (e ? { ...e, id } : e))}
          onSaved={(text) => onToast({ text, tone: "success" })}
        />
      )}
      <RetireDialog
        open={retiring !== null}
        name={retiring?.name ?? ""}
        onClose={() => setRetiring(null)}
        onConfirm={async (retirement) => {
          if (!retiring) return null;
          const failure = await retireEntity("category", retiring.id, retiring.name, retirement);
          if (failure) return failure;
          onToast({ text: `Đã ngừng bán ${retiring.name}`, tone: "success" });
          setRetiring(null);
          return null;
        }}
      />
    </div>
  );
}
