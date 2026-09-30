import { useCallback, useRef, useState, type ReactElement } from "react";
import { useHotkeys } from "react-hotkeys-hook";
import { Button } from "@/components/ui/button";
import { StockToast, type StockToastMessage } from "@/components/feedback/stock-toast";
import { messageForError } from "@/lib/error-messages";
import { useCatalogModel } from "../api/use-catalog-admin";
import type { CatalogViewKey } from "../lib/views";
import { CategoriesView } from "./categories-view";
import { CatalogHeader, CatalogPills } from "./catalog-nav";
import { GroupsView } from "./groups-view";
import { ItemFormModal } from "./item-form-modal";
import { ItemsView } from "./items-view";
import { LinkerView } from "./linker-view";

export interface CatalogViewProps {
  view: CatalogViewKey;
  onViewChange: (view: CatalogViewKey) => void;
}

/** The "Quản lý Thực đơn & Topping" tab. It hosts the item modal so the hero button and Ctrl+N reach it from any view. */
export function CatalogView({ view, onViewChange }: CatalogViewProps): ReactElement {
  const { model, isPending, error, refetch } = useCatalogModel();
  const [toast, setToast] = useState<StockToastMessage | null>(null);
  const [itemEditor, setItemEditor] = useState<{ id: string | null; session: number } | null>(null);
  const sessions = useRef(0);
  const dismissToast = useCallback(() => setToast(null), []);
  const openItem = useCallback((id: string | null) => setItemEditor({ id, session: ++sessions.current }), []);
  const canAdd = model.categories.length > 0;
  const newItem = () => {
    onViewChange("items");
    openItem(null);
  };

  useHotkeys(
    "mod+n",
    (event) => {
      event.preventDefault();
      if (canAdd) newItem();
    },
    { enabled: itemEditor === null && !isPending },
  );

  if (isPending) {
    return (
      <div className="space-y-5">
        <div className="h-[92px] animate-pulse rounded-2xl bg-slate-200/60 dark:bg-muted" />
        <div className="h-16 animate-pulse rounded-2xl bg-slate-200/60 dark:bg-muted" />
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 6 }, (_, i) => (
            <div key={i} className="h-[114px] animate-pulse rounded-2xl bg-slate-200/60 dark:bg-muted" />
          ))}
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center gap-3 rounded-2xl border border-slate-200 bg-card p-12 text-center dark:border-border">
        <p role="alert" className="text-sm font-semibold text-destructive">
          {messageForError(error)}
        </p>
        <Button onClick={refetch} className="h-12 min-h-[48px] rounded-xl px-6">
          Thử lại
        </Button>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <CatalogHeader canAdd={canAdd} onNewItem={newItem} />
      <CatalogPills
        view={view}
        counts={{ items: model.items.length, categories: model.categories.length, groups: model.groups.length }}
        onChange={onViewChange}
      />
      {view === "items" && <ItemsView model={model} onOpenItem={openItem} />}
      {view === "categories" && <CategoriesView model={model} onToast={setToast} />}
      {view === "groups" && <GroupsView model={model} onToast={setToast} />}
      {view === "linker" && <LinkerView model={model} onToast={setToast} />}
      {itemEditor && (
        <ItemFormModal
          key={itemEditor.session}
          snapshot={model.items.find((i) => i.id === itemEditor.id) ?? null}
          model={model}
          defaultCategoryId={model.categories[0]?.id ?? ""}
          onClose={() => setItemEditor(null)}
          onCreated={(id) => setItemEditor((e) => (e ? { ...e, id } : e))}
          onSaved={(text) => setToast({ text, tone: "success" })}
        />
      )}
      <StockToast toast={toast} onDismiss={dismissToast} />
    </div>
  );
}
