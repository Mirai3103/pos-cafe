import type {
  CatalogSellableCategoryResponse,
  CatalogSellableItemResponse,
} from "@/api/generated/models";
import { matchesSearch } from "@/lib/search";

export { normalizeVietnamese, getAcronym, matchesSearch } from "@/lib/search";

/**
 * Filters items from categories by selected category ID and search query.
 */
export function filterSellableItems(
  categories: CatalogSellableCategoryResponse[],
  selectedCategoryId: string | null,
  searchQuery: string,
): CatalogSellableItemResponse[] {
  if (!categories) return [];

  const targetCategories = selectedCategoryId
    ? categories.filter((cat) => cat.id === selectedCategoryId)
    : categories;

  const result: CatalogSellableItemResponse[] = [];

  for (const cat of targetCategories) {
    const items = cat.items ?? [];
    for (const item of items) {
      if (item.name && matchesSearch(searchQuery, item.name, cat.name)) {
        result.push(item);
      }
    }
  }

  return result;
}
