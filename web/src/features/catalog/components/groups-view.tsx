// web/src/features/catalog/components/groups-view.tsx
import { useRef, useState, type ReactElement } from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";
import type { StockToastMessage } from "@/features/settings/components/stock-toast";
import { formatVND } from "@/lib/utils";
import { useRetireEntity } from "../api/use-catalog-admin";
import { currentAssignment, ruleLabel, type CatalogModel, type CatGroup } from "../lib/catalog-model";
import { GroupFormModal } from "./group-form-modal";
import { RetireDialog } from "./retire-dialog";

export interface GroupsViewProps {
  model: CatalogModel;
  onToast: (toast: StockToastMessage) => void;
}

export function GroupsView({ model, onToast }: GroupsViewProps): ReactElement {
  const [editing, setEditing] = useState<{ id: string | null; session: number } | null>(null);
  const [retiring, setRetiring] = useState<CatGroup | null>(null);
  const sessions = useRef(0);
  const retireEntity = useRetireEntity();
  const open = (id: string | null) => setEditing({ id, session: ++sessions.current });

  return (
    <div className="space-y-5">
      <div className="flex flex-col justify-between gap-3 rounded-2xl border border-slate-200 bg-card p-4 shadow-2xs sm:flex-row sm:items-center dark:border-border">
        <div>
          <h2 className="text-sm font-bold text-slate-900 sm:text-base dark:text-foreground">Nhóm Topping & Tùy chọn Món</h2>
          <p className="text-xs text-slate-500 dark:text-muted-foreground">
            Thiết lập các nhóm tùy chọn (Đường, Đá, Topping, Kem Phô Mai), số lượng chọn tối thiểu/tối đa và đơn giá từng loại.
          </p>
        </div>
        <button
          type="button"
          onClick={() => open(null)}
          className="flex h-12 min-h-[48px] shrink-0 items-center justify-center gap-2 rounded-xl bg-emerald-600 px-5 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 active:scale-[0.98] sm:text-sm"
        >
          <Plus className="h-4 w-4" />+ TẠO NHÓM TOPPING
        </button>
      </div>

      <p className="text-xs text-slate-500 dark:text-muted-foreground">
        Hiển thị: <span className="font-mono font-bold text-slate-900 dark:text-foreground">{model.groups.length} nhóm</span>
      </p>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {model.groups.map((g) => {
          const usage = currentAssignment(model, g.id);
          return (
            <div key={g.id} className="flex flex-col gap-3 rounded-2xl border border-slate-200 bg-card p-4 shadow-2xs dark:border-border">
              <div className="flex items-start justify-between gap-3">
                <div>
                  <p className="text-sm font-bold text-slate-900 dark:text-foreground">{g.name}</p>
                  <p className="text-[11px] text-slate-500">
                    {ruleLabel(g)} · Dùng cho {usage.itemIds.length} món · {usage.categoryIds.length} danh mục
                  </p>
                </div>
                <div className="flex gap-1.5">
                  <button
                    type="button"
                    aria-label={`Sửa ${g.name}`}
                    onClick={() => open(g.id)}
                    className="flex h-12 min-h-[48px] w-12 min-w-[48px] items-center justify-center rounded-xl border border-slate-200 text-slate-600 hover:bg-slate-50 dark:border-border"
                  >
                    <Pencil className="h-4 w-4" />
                  </button>
                  <button
                    type="button"
                    aria-label={`Ngừng bán ${g.name}`}
                    onClick={() => setRetiring(g)}
                    className="flex h-12 min-h-[48px] w-12 min-w-[48px] items-center justify-center rounded-xl border border-rose-200 bg-rose-50 text-rose-700 hover:bg-rose-100 dark:border-rose-900 dark:bg-rose-950/30"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </div>
              <ul className="space-y-1">
                {g.options.map((o) => (
                  <li key={o.id} className="flex items-center justify-between text-xs">
                    <span className="text-slate-700 dark:text-foreground">
                      {o.name}
                      {g.defaultOptionIds.includes(o.id) && <span className="ml-1.5 text-[10px] font-bold text-emerald-600">Mặc định</span>}
                    </span>
                    <span className="font-mono text-slate-500">{o.surchargeVnd > 0 ? `+${formatVND(o.surchargeVnd)}` : "0"}</span>
                  </li>
                ))}
              </ul>
            </div>
          );
        })}
      </div>

      {editing && (
        <GroupFormModal
          key={editing.session}
          snapshot={model.groups.find((g) => g.id === editing.id) ?? null}
          onClose={() => setEditing(null)}
          onSaved={(text) => onToast({ text, tone: "success" })}
        />
      )}
      <RetireDialog
        open={retiring !== null}
        name={retiring?.name ?? ""}
        onClose={() => setRetiring(null)}
        onConfirm={async (retirement) => {
          if (!retiring) return null;
          const failure = await retireEntity("group", retiring.id, retiring.name, retirement);
          if (failure) return failure;
          onToast({ text: `Đã ngừng bán ${retiring.name}`, tone: "success" });
          setRetiring(null);
          return null;
        }}
      />
    </div>
  );
}
