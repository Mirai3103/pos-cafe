// web/src/features/catalog/components/linker-view.tsx
import { useState, type ReactElement } from "react";
import { MonitorSmartphone, Search, Zap } from "lucide-react";
import { useHotkeys } from "react-hotkeys-hook";
import type { StockToastMessage } from "@/components/feedback/stock-toast";
import { ItemPickerBody } from "@/features/pos";
import { cn } from "@/lib/utils";
import { useCatalogSave } from "../api/use-catalog-admin";
import { currentAssignment, filterItems, type Assignment, type CatalogModel } from "../lib/catalog-model";
import { clearCategoryItems, linkState, sameAssignment, selectAllInCategory, toggleCategory, toggleItem } from "../lib/linker";
import { planAssignments } from "../lib/save-plan";
import { toSellablePreview } from "../lib/sellable-preview";
import { INPUT_CLASS } from "./form-bits";

export interface LinkerViewProps {
  model: CatalogModel;
  onToast: (toast: StockToastMessage) => void;
}

const STATE_LABEL = { excluded: "Đang loại trừ, bỏ loại trừ trong form Món", inherited: "Kế thừa", direct: "", none: "" } as const;

export function LinkerView({ model, onToast }: LinkerViewProps): ReactElement {
  const [groupId, setGroupId] = useState<string | null>(null);
  const [draft, setDraft] = useState<Assignment | null>(null);
  const [focusId, setFocusId] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const { save, isSaving } = useCatalogSave();

  const group = model.groups.find((g) => g.id === groupId) ?? model.groups[0] ?? null;
  const current = group ? currentAssignment(model, group.id) : { itemIds: [], categoryIds: [] };
  const pending = draft ?? current;
  const dirty = !sameAssignment(current, pending);

  const apply = async () => {
    if (!group || !dirty || isSaving) return;
    const outcome = await save(planAssignments(group.id, group.name, current, pending));
    if (outcome.cancelled) return;
    if (outcome.ok) {
      setDraft(null);
      onToast({ text: `Đã áp dụng ${group.name}`, tone: "success" });
    } else {
      onToast({ text: outcome.results.find((r) => r.status === "failed")?.error ?? "Không thể lưu", tone: "error" });
    }
  };

  useHotkeys(
    "mod+s",
    (event) => {
      event.preventDefault();
      void apply();
    },
    { enableOnFormTags: true },
  );

  if (!group) {
    return (
      <p className="rounded-2xl border border-dashed border-slate-300 p-10 text-center text-sm text-slate-500 dark:border-border">
        Chưa có nhóm topping nào. Tạo nhóm ở mục Nhóm Topping.
      </p>
    );
  }

  const selectGroup = (id: string) => {
    if (id === group.id) return;
    if (dirty && !window.confirm("Bỏ các thay đổi chưa lưu của nhóm này?")) return;
    setGroupId(id);
    setDraft(null);
  };
  const edit = (next: Assignment) => setDraft(next);

  const visible = filterItems(model, query, "all");
  const focusItem = model.items.find((i) => i.id === focusId) ?? visible[0] ?? null;
  const preview = focusItem ? toSellablePreview(focusItem, model, { groupId: group.id, ...pending }) : null;
  const previewKey = preview ? `${preview.id}:${(preview.modifier_groups ?? []).map((g) => g.id).join(",")}` : "none";

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-card p-4 shadow-2xs dark:border-border">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-amber-50 text-amber-600 dark:bg-amber-950/40">
          <Zap className="h-5 w-5" />
        </div>
        <div>
          <h2 className="text-sm font-bold text-slate-900 sm:text-base dark:text-foreground">Ma trận Gán Topping Hàng Loạt (Batch Linker)</h2>
          <p className="text-xs text-slate-500 dark:text-muted-foreground">
            Chọn 1 nhóm topping, tick nhanh theo danh mục hoặc nhiều món cùng lúc, và xem trước mô phỏng màn hình thu ngân tức thì.
          </p>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[28fr_42fr_30fr]">
        <section className="space-y-2 rounded-2xl border border-slate-200 bg-card p-3 dark:border-border">
          <p className="px-1 text-xs font-bold text-slate-700 dark:text-foreground">1. Nhóm Topping</p>
          {model.groups.map((g) => (
            <button
              key={g.id}
              type="button"
              onClick={() => selectGroup(g.id)}
              className={cn(
                "flex min-h-[48px] w-full items-center justify-between gap-2 rounded-xl border px-3 py-2 text-left text-xs transition",
                g.id === group.id
                  ? "border-emerald-600 bg-emerald-50 font-bold text-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-300"
                  : "border-slate-200 text-slate-700 hover:bg-slate-50 dark:border-border dark:text-foreground dark:hover:bg-muted",
              )}
            >
              <span>{g.name}</span>
              <span className="font-mono text-3xs text-slate-400">{currentAssignment(model, g.id).itemIds.length} món</span>
            </button>
          ))}
        </section>

        <section className="flex flex-col rounded-2xl border border-slate-200 bg-card dark:border-border">
          <div className="space-y-2 border-b border-slate-100 p-3 dark:border-border">
            <p className="text-xs font-bold text-slate-700 dark:text-foreground">
              2. Danh mục & Món áp dụng · Nhóm: <span className="text-emerald-700">{group.name}</span>
            </p>
            <div className="relative">
              <Search className="pointer-events-none absolute top-1/2 left-3.5 h-4 w-4 -translate-y-1/2 text-slate-400" />
              <input
                aria-label="Tìm món"
                className={`${INPUT_CLASS} pl-10`}
                value={query}
                placeholder="Tìm món theo tên hoặc viết tắt (vd: cfsd, tdcs)..."
                onChange={(e) => setQuery(e.target.value)}
              />
            </div>
          </div>
          <div className="max-h-[60vh] flex-1 space-y-3 overflow-y-auto p-3">
            {model.categories.map((c) => {
              const items = visible.filter((i) => i.categoryId === c.id);
              if (items.length === 0 && query) return null;
              return (
                <div key={c.id} className="space-y-1.5">
                  <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-slate-50 px-2 py-1 dark:bg-muted/40">
                    <label className="flex min-h-[48px] cursor-pointer items-center gap-2 text-xs font-bold text-slate-800 dark:text-foreground">
                      <input
                        type="checkbox"
                        className="h-5 w-5 accent-emerald-600"
                        checked={pending.categoryIds.includes(c.id)}
                        onChange={() => edit(toggleCategory(pending, c.id))}
                      />
                      {c.name} · Gán cho cả danh mục
                    </label>
                    <span className="text-2xs text-slate-500">
                      <button type="button" className="min-h-[48px] px-1 font-semibold text-emerald-700" onClick={() => edit(selectAllInCategory(pending, model, group.id, c.id))}>
                        Chọn hết
                      </button>
                      |
                      <button type="button" className="min-h-[48px] px-1 font-semibold text-slate-500" onClick={() => edit(clearCategoryItems(pending, model, c.id))}>
                        Bỏ chọn hết
                      </button>
                    </span>
                  </div>
                  {items.map((i) => {
                    const state = linkState(i, group.id, pending);
                    return (
                      <label
                        key={i.id}
                        onMouseEnter={() => setFocusId(i.id)}
                        className={cn(
                          "flex min-h-[48px] cursor-pointer items-center gap-3 rounded-xl border px-3 text-xs",
                          focusItem?.id === i.id ? "border-emerald-300" : "border-transparent",
                          state === "excluded" && "cursor-not-allowed opacity-60",
                        )}
                      >
                        <input
                          type="checkbox"
                          className="h-5 w-5 accent-emerald-600"
                          disabled={state === "excluded"}
                          checked={state === "direct"}
                          onChange={() => {
                            setFocusId(i.id);
                            edit(toggleItem(pending, i.id));
                          }}
                        />
                        <span className="flex-1 font-semibold text-slate-800 dark:text-foreground">{i.name}</span>
                        {STATE_LABEL[state] && <span className="text-3xs font-semibold text-slate-500">{STATE_LABEL[state]}</span>}
                      </label>
                    );
                  })}
                </div>
              );
            })}
          </div>
          <div className="sticky bottom-0 flex flex-wrap items-center justify-between gap-3 border-t border-slate-100 bg-card p-3 dark:border-border">
            <span className="text-xs text-slate-500">
              Đã chọn: <span className="font-mono font-bold text-slate-900 dark:text-foreground">{pending.itemIds.length}</span> món cho nhóm{" "}
              {group.name}
            </span>
            <button
              type="button"
              disabled={!dirty || isSaving}
              onClick={() => void apply()}
              className="flex h-14 min-h-[48px] items-center gap-2 rounded-xl bg-emerald-600 px-6 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 active:scale-[0.98] disabled:opacity-50 sm:text-sm"
            >
              {isSaving ? "Đang lưu…" : "LƯU ÁP DỤNG NGAY (Ctrl+S)"}
            </button>
          </div>
        </section>

        <section className="flex flex-col overflow-hidden rounded-2xl border border-slate-200 bg-card dark:border-border">
          <div className="flex items-center gap-2 border-b border-slate-100 p-3 dark:border-border">
            <MonitorSmartphone className="h-4 w-4 text-emerald-600" />
            <p className="text-xs font-bold text-slate-700 dark:text-foreground">Mô phỏng màn hình thu ngân</p>
            <span className="ml-auto rounded-full bg-emerald-50 px-2 py-0.5 text-3xs font-bold text-emerald-700">Live POS Sync</span>
          </div>
          {preview ? (
            <div className="flex max-h-[70vh] flex-col">
              <p className="px-4 pt-3 text-sm font-bold text-slate-900 dark:text-foreground">{preview.name}</p>
              <ItemPickerBody key={previewKey} item={preview} onConfirm={() => {}} isSubmitting confirmLabel="Chỉ xem trước" />
            </div>
          ) : (
            <p className="p-6 text-center text-xs text-slate-400">Chọn một món để xem trước.</p>
          )}
        </section>
      </div>
    </div>
  );
}
