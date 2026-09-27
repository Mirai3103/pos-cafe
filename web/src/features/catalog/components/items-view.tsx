// web/src/features/catalog/components/items-view.tsx
import { useState, type ReactElement } from "react";
import { Plus, Search, X } from "lucide-react";
import { filterItems, type CatalogModel } from "../lib/catalog-model";
import { effectiveGroupIds, inheritedGroupIds } from "../lib/item-inheritance";
import { INPUT_CLASS } from "./form-bits";
import { ItemCard } from "./item-card";

export interface ItemsViewProps {
  model: CatalogModel;
  /** Null creates a new item. */
  onOpenItem: (itemId: string | null) => void;
}

export function ItemsView({ model, onOpenItem }: ItemsViewProps): ReactElement {
  const [query, setQuery] = useState("");
  const [categoryId, setCategoryId] = useState("all");
  const visible = filterItems(model, query, categoryId);
  const categoryName = new Map(model.categories.map((c) => [c.id, c.name]));
  const canAdd = model.categories.length > 0;

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-3 rounded-2xl border border-slate-200 bg-card p-4 shadow-2xs lg:flex-row lg:items-center dark:border-border">
        <div className="flex flex-1 flex-col gap-3 sm:flex-row">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute top-1/2 left-3.5 h-4 w-4 -translate-y-1/2 text-slate-400" />
            <input
              aria-label="Tìm món"
              className={`${INPUT_CLASS} pr-12 pl-10`}
              value={query}
              placeholder="Tìm món nhanh (tên món, viết tắt: cfsd, tdcs)..."
              onChange={(e) => setQuery(e.target.value)}
            />
            {query && (
              <button
                type="button"
                aria-label="Xóa tìm kiếm"
                onClick={() => setQuery("")}
                className="absolute top-0 right-0 flex h-12 w-12 items-center justify-center text-slate-400 hover:text-slate-600"
              >
                <X className="h-4 w-4" />
              </button>
            )}
          </div>
          <select
            aria-label="Lọc theo danh mục"
            className={`${INPUT_CLASS} sm:w-56`}
            value={categoryId}
            onChange={(e) => setCategoryId(e.target.value)}
          >
            <option value="all">Tất cả danh mục</option>
            {model.categories.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </div>
        <button
          type="button"
          disabled={!canAdd}
          title={canAdd ? undefined : "Tạo danh mục trước"}
          onClick={() => onOpenItem(null)}
          className="flex h-12 min-h-[48px] items-center justify-center gap-2 rounded-xl bg-emerald-600 px-5 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 active:scale-[0.98] disabled:opacity-50 sm:text-sm"
        >
          <Plus className="h-4 w-4" />+ THÊM MÓN MỚI (Ctrl+N)
        </button>
      </div>

      <p className="text-xs text-slate-500 dark:text-muted-foreground">
        Hiển thị: <span className="font-mono font-bold text-slate-900 dark:text-foreground">{visible.length} món</span>
      </p>

      {model.items.length === 0 ? (
        <p className="rounded-2xl border border-dashed border-slate-300 p-10 text-center text-sm text-slate-500 dark:border-border">
          Chưa có món nào trong thực đơn
        </p>
      ) : visible.length === 0 ? (
        <p className="rounded-2xl border border-dashed border-slate-300 p-10 text-center text-sm text-slate-500 dark:border-border">
          Không tìm thấy món phù hợp
        </p>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {visible.map((item) => (
            <ItemCard
              key={item.id}
              item={item}
              categoryName={categoryName.get(item.categoryId) ?? ""}
              groupCount={
                effectiveGroupIds(inheritedGroupIds(model.categories, item.categoryId), item.directGroupIds, item.excludedGroupIds)
                  .length
              }
              onOpen={() => onOpenItem(item.id)}
            />
          ))}
        </div>
      )}
    </div>
  );
}
