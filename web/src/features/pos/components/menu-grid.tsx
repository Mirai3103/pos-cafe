import * as React from "react";
import { Search, X, Coffee } from "lucide-react";
import type {
  CatalogSellableCategoryResponse,
  CatalogSellableItemResponse,
} from "@/api/generated/models";
import { MenuItemCard } from "./menu-item-card";
import { filterSellableItems } from "../utils/search";
import { playTapChirp } from "@/lib/sound";

export interface MenuGridProps {
  categories?: CatalogSellableCategoryResponse[];
  onSelectItem: (item: CatalogSellableItemResponse) => void;
  disabled?: boolean;
}

export function MenuGrid({
  categories = [],
  onSelectItem,
  disabled = false,
}: MenuGridProps) {
  const [selectedCategoryId, setSelectedCategoryId] = React.useState<string | null>(
    null,
  );
  const [searchQuery, setSearchQuery] = React.useState("");
  const searchInputRef = React.useRef<HTMLInputElement>(null);

  // Global shortcut '/' to focus search input
  React.useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (
        e.key === "/" &&
        document.activeElement?.tagName !== "INPUT" &&
        document.activeElement?.tagName !== "TEXTAREA"
      ) {
        e.preventDefault();
        searchInputRef.current?.focus();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  const totalItemCount = React.useMemo(() => {
    return categories.reduce((acc, cat) => acc + (cat.items?.length ?? 0), 0);
  }, [categories]);

  const filteredItems = React.useMemo(() => {
    return filterSellableItems(categories, selectedCategoryId, searchQuery);
  }, [categories, selectedCategoryId, searchQuery]);

  // Find category name for item
  const categoryNameMap = React.useMemo(() => {
    const map = new Map<string, string>();
    for (const cat of categories) {
      for (const item of cat.items ?? []) {
        if (item.id && cat.name) {
          map.set(item.id, cat.name);
        }
      }
    }
    return map;
  }, [categories]);

  return (
    <section className="flex flex-1 flex-col overflow-hidden bg-background">
      {/* Top Search & Category Filter Toolbar */}
      <div className="flex flex-col gap-2.5 border-b border-border bg-card p-4 shrink-0 shadow-2xs">
        {/* Search Input Bar */}
        <div className="relative w-full">
          <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
          <input
            ref={searchInputRef}
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Tìm món nhanh (tên món, viết tắt: cfsd, bx)... [/]"
            className="h-12 w-full rounded-xl border border-border bg-muted/40 pl-10 pr-12 text-sm font-medium text-foreground placeholder:text-muted-foreground focus:border-primary focus:bg-card focus:outline-none focus:ring-2 focus:ring-primary/20 transition-all"
          />
          {searchQuery ? (
            <button
              type="button"
              onClick={() => {
                setSearchQuery("");
                searchInputRef.current?.focus();
              }}
              className="absolute right-2 top-1/2 -translate-y-1/2 h-8 w-8 rounded-lg flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted"
            >
              <X className="h-4 w-4" />
            </button>
          ) : (
            <kbd className="absolute right-3 top-1/2 -translate-y-1/2 pointer-events-none rounded border border-border bg-card px-1.5 py-0.5 font-mono text-2xs text-muted-foreground shadow-2xs">
              /
            </kbd>
          )}
        </div>

        {/* Category Filter Pills Rail (min-h-[48px]) */}
        <div className="flex items-center gap-2 overflow-x-auto no-scrollbar py-0.5">
          {/* 'Tất cả' Pill */}
          <button
            type="button"
            onClick={() => {
              playTapChirp();
              setSelectedCategoryId(null);
            }}
            className={`min-h-[48px] h-12 px-4 shrink-0 inline-flex items-center gap-2 rounded-xl text-sm font-semibold transition-all select-none active:scale-[0.98] ${
              selectedCategoryId === null
                ? "bg-primary text-primary-foreground shadow-xs"
                : "border border-border bg-card text-foreground hover:bg-muted"
            }`}
          >
            <span>Tất cả</span>
            <span
              className={`rounded-full px-2 py-0.5 text-2xs font-mono font-bold ${
                selectedCategoryId === null
                  ? "bg-primary-foreground/20 text-primary-foreground"
                  : "bg-muted text-muted-foreground"
              }`}
            >
              {totalItemCount}
            </span>
          </button>

          {/* Individual Category Pills */}
          {categories.map((cat) => {
            const isSelected = selectedCategoryId === cat.id;
            const count = cat.items?.length ?? 0;
            return (
              <button
                key={cat.id}
                type="button"
                onClick={() => {
                  playTapChirp();
                  setSelectedCategoryId(cat.id ?? null);
                }}
                className={`min-h-[48px] h-12 px-4 shrink-0 inline-flex items-center gap-2 rounded-xl text-sm font-semibold transition-all select-none active:scale-[0.98] ${
                  isSelected
                    ? "bg-primary text-primary-foreground shadow-xs"
                    : "border border-border bg-card text-foreground hover:bg-muted"
                }`}
              >
                <span>{cat.name}</span>
                <span
                  className={`rounded-full px-2 py-0.5 text-2xs font-mono font-bold ${
                    isSelected
                      ? "bg-primary-foreground/20 text-primary-foreground"
                      : "bg-muted text-muted-foreground"
                  }`}
                >
                  {count}
                </span>
              </button>
            );
          })}
        </div>
      </div>

      {/* Product Card Grid Container */}
      <div className="flex-1 overflow-y-auto p-4">
        {filteredItems.length > 0 ? (
          <div className="grid grid-cols-2 sm:grid-cols-3 xl:grid-cols-4 gap-3">
            {filteredItems.map((item) => (
              <MenuItemCard
                key={item.id}
                item={item}
                categoryName={item.id ? categoryNameMap.get(item.id) : undefined}
                onSelect={onSelectItem}
                disabled={disabled}
              />
            ))}
          </div>
        ) : (
          <div className="flex h-full flex-col items-center justify-center p-8 text-center text-muted-foreground gap-3">
            <div className="h-16 w-16 rounded-2xl bg-muted/60 flex items-center justify-center text-muted-foreground/60 border border-border">
              <Coffee className="h-8 w-8" />
            </div>
            <div className="space-y-1">
              <p className="text-sm font-bold text-foreground">Không tìm thấy món phù hợp</p>
              <p className="text-xs text-muted-foreground">
                Thử đổi từ khóa tìm kiếm hoặc chọn danh mục khác
              </p>
            </div>
          </div>
        )}
      </div>
    </section>
  );
}
