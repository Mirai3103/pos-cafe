import type { ReactElement } from "react";
import { Cake, Coffee, CupSoda, Layers, Sparkles, Utensils, type LucideIcon } from "lucide-react";
import type { StockIcon } from "../lib/availability-cards";

const ICONS: Record<StockIcon, LucideIcon> = {
  coffee: Coffee,
  "cup-soda": CupSoda,
  sparkles: Sparkles,
  cake: Cake,
  layers: Layers,
  utensils: Utensils,
};

export function StockIconGlyph({ icon, className }: { icon: StockIcon; className?: string }): ReactElement {
  const Icon = ICONS[icon];
  return <Icon className={className} />;
}
