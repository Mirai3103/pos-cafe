export interface PriceableItem {
  price_vnd?: number;
  sizes?: Array<{ price_vnd?: number }>;
}

export interface DraftItemLike {
  price_vnd?: number;
  quantity?: number;
  selected_modifier_options?: Array<{ surcharge_vnd?: number }>;
}

/**
 * Returns the base starting price for a menu item.
 * If the item has sizes, returns the minimum available size price.
 */
export function getStartingPrice(item: PriceableItem): number {
  if (item.sizes && item.sizes.length > 0) {
    const validPrices = item.sizes
      .map((s) => s.price_vnd ?? 0)
      .filter((p) => p > 0);
    if (validPrices.length > 0) {
      return Math.min(...validPrices);
    }
  }
  return item.price_vnd ?? 0;
}

/**
 * Computes configured item unit price by adding all selected modifier surcharges
 * to the base item or size price.
 */
export function calculateItemUnitPrice(
  basePrice: number,
  optionSurcharges: number[],
): number {
  const surchargesTotal = optionSurcharges.reduce((acc, curr) => acc + curr, 0);
  return basePrice + surchargesTotal;
}

/**
 * Computes total price for a line item given its unit price and quantity.
 */
export function calculateLineTotal(unitPrice: number, quantity: number): number {
  return unitPrice * Math.max(1, quantity);
}

/**
 * Computes total draft subtotal across all draft line items.
 */
export function calculateDraftSubtotal(items?: DraftItemLike[]): number {
  if (!items || items.length === 0) return 0;

  return items.reduce((total, item) => {
    const base = item.price_vnd ?? 0;
    const surcharges = (item.selected_modifier_options ?? []).map(
      (opt) => opt.surcharge_vnd ?? 0,
    );
    const unitPrice = calculateItemUnitPrice(base, surcharges);
    return total + calculateLineTotal(unitPrice, item.quantity ?? 1);
  }, 0);
}
