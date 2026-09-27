// web/src/features/catalog/lib/category-icons.ts
import {
  Apple,
  Bean,
  Beer,
  Cake,
  Cherry,
  Citrus,
  Coffee,
  Cookie,
  Croissant,
  CupSoda,
  GlassWater,
  IceCreamBowl,
  IceCreamCone,
  LayoutGrid,
  Leaf,
  Milk,
  Nut,
  Sandwich,
  Soup,
  UtensilsCrossed,
  Wine,
  type LucideIcon,
} from "lucide-react";

/** The web owns the offered set (BA-1 §2.2); keys are stored as the category's icon. */
export const CATEGORY_ICONS: Record<string, LucideIcon> = {
  coffee: Coffee,
  bean: Bean,
  leaf: Leaf,
  citrus: Citrus,
  milk: Milk,
  "glass-water": GlassWater,
  cherry: Cherry,
  apple: Apple,
  "cup-soda": CupSoda,
  "ice-cream-bowl": IceCreamBowl,
  "ice-cream-cone": IceCreamCone,
  croissant: Croissant,
  cake: Cake,
  cookie: Cookie,
  sandwich: Sandwich,
  soup: Soup,
  nut: Nut,
  wine: Wine,
  beer: Beer,
  "utensils-crossed": UtensilsCrossed,
};

export const CATEGORY_ICON_NAMES = Object.keys(CATEGORY_ICONS);

export function categoryIcon(name: string | null): LucideIcon {
  return (name && CATEGORY_ICONS[name]) || LayoutGrid;
}
