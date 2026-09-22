import type {
  CatalogSellableCategoryResponse,
  CatalogSellableItemResponse,
} from "@/api/generated/models";

/**
 * Normalizes Vietnamese string: removes diacritics, converts d-stroke to d, lowercases,
 * and collapses multiple whitespace characters.
 */
export function normalizeVietnamese(text: string): string {
  if (!text) return "";
  return text
    .toLowerCase()
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .replace(/đ/g, "d")
    .replace(/Đ/g, "d")
    .replace(/\s+/g, " ")
    .trim();
}

/**
 * Generates first-letter acronym for a name.
 * Applies Vietnamese cafe convention ('ca' + 'phe' -> 'cf', e.g. 'Cà phê sữa đá' -> 'cfsd').
 * Ignores punctuation and formats as lowercase acronym.
 */
export function getAcronym(text: string): string {
  if (!text) return "";
  const normalized = normalizeVietnamese(text);
  const words = normalized.split(/[^a-z0-9]+/).filter(Boolean);
  if (words.length === 0) return "";

  // Vietnamese cafe convention: 'ca' + 'phe' -> 'cf'
  if (words.length >= 2 && words[0] === "ca" && words[1] === "phe") {
    return ("cf" + words.slice(2).map((w) => w[0]).join("")).toLowerCase();
  }

  return words.map((w) => w[0]).join("").toLowerCase();
}

/**
 * Checks whether an item matches a search query by normalized substring or acronym.
 * Supports direct name matches, cafe acronyms, direct first-letter acronyms, and category names.
 */
export function matchesSearch(
  query: string,
  itemName: string,
  categoryName?: string,
): boolean {
  const q = normalizeVietnamese(query);
  if (!q) return true;

  const normalizedName = normalizeVietnamese(itemName);
  if (normalizedName.includes(q)) return true;

  // Compare query against acronyms (removing spaces in query if user typed spaced initials)
  const qAcronym = q.replace(/\s+/g, "");
  const cafeAcronym = getAcronym(itemName);
  if (cafeAcronym && cafeAcronym.includes(qAcronym)) return true;

  // Also support direct first-letter acronym if different from cafe convention
  const words = normalizedName.split(/[^a-z0-9]+/).filter(Boolean);
  const directAcronym = words.map((w) => w[0]).join("");
  if (directAcronym && directAcronym.includes(qAcronym)) return true;

  // Match without punctuation for queries on punctuation-containing names
  const cleanName = normalizedName.replace(/[^a-z0-9\s]/g, " ").replace(/\s+/g, " ").trim();
  const cleanQuery = q.replace(/[^a-z0-9\s]/g, " ").replace(/\s+/g, " ").trim();
  if (cleanName && cleanQuery && cleanName.includes(cleanQuery)) return true;

  if (categoryName) {
    const normalizedCategory = normalizeVietnamese(categoryName);
    if (normalizedCategory.includes(q)) return true;

    const catAcronym = getAcronym(categoryName);
    if (catAcronym && catAcronym.includes(qAcronym)) return true;
  }

  return false;
}

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
