import * as React from "react";
import { Coffee, Layers } from "lucide-react";
import type { CatalogSellableItemResponse } from "@/api/generated/models";
import { getStartingPrice } from "../utils/pricing";
import { formatVND } from "@/lib/utils";
import { playTapChirp } from "@/lib/sound";

export interface MenuItemCardProps {
  item: CatalogSellableItemResponse;
  categoryName?: string;
  onSelect: (item: CatalogSellableItemResponse) => void;
  disabled?: boolean;
}

export function MenuItemCard({
  item,
  categoryName,
  onSelect,
  disabled = false,
}: MenuItemCardProps) {
  const [imageError, setImageError] = React.useState(false);
  const startingPrice = getStartingPrice(item);
  const hasSizes = Boolean(item.sizes && item.sizes.length > 0);

  const handleClick = () => {
    if (disabled) return;
    playTapChirp();
    onSelect(item);
  };

  return (
    <div
      role="button"
      tabIndex={disabled ? -1 : 0}
      onClick={handleClick}
      onKeyDown={(e) => {
        if (!disabled && (e.key === "Enter" || e.key === " ")) {
          e.preventDefault();
          handleClick();
        }
      }}
      className={`group flex flex-col justify-between rounded-2xl border border-border bg-card p-3 min-h-[210px] select-none transition-all ${
        disabled
          ? "opacity-50 cursor-not-allowed"
          : "cursor-pointer hover:border-primary hover:shadow-xs active:scale-[0.98]"
      }`}
    >
      {/* Media Well with Fallback */}
      <div className="relative aspect-[16/10] min-h-[110px] w-full overflow-hidden rounded-xl bg-muted flex items-center justify-center text-muted-foreground">
        {item.image_url && !imageError ? (
          <img
            src={item.image_url}
            alt={item.name ?? "Món"}
            className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
            onError={() => setImageError(true)}
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center bg-muted/60 text-muted-foreground">
            <Coffee className="h-8 w-8" />
          </div>
        )}

        {/* Multi-size indicator badge */}
        {hasSizes && (
          <div className="absolute bottom-2 right-2 flex items-center gap-1 rounded-md bg-background/90 backdrop-blur-xs px-2 py-0.5 text-2xs font-semibold text-foreground border border-border shadow-2xs">
            <Layers className="h-3 w-3 text-primary" />
            <span>{item.sizes?.length} cỡ</span>
          </div>
        )}
      </div>

      {/* Item Body */}
      <div className="mt-2.5 flex flex-col">
        {categoryName && (
          <span className="text-2xs font-semibold uppercase tracking-wider text-muted-foreground truncate">
            {categoryName}
          </span>
        )}
        <h4 className="text-sm font-semibold text-foreground line-clamp-1 leading-tight mt-0.5">
          {item.name}
        </h4>
      </div>

      {/* Price Readout */}
      <div className="mt-2 flex items-baseline justify-between pt-1 border-t border-border/50">
        <span className="font-mono text-sm sm:text-base font-bold text-primary">
          {hasSizes ? `Từ ${formatVND(startingPrice)}` : formatVND(startingPrice)}
        </span>
      </div>
    </div>
  );
}
