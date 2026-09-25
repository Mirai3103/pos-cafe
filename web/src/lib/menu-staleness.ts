import { ApiError } from "./unwrap";

/**
 * Refusals that mean this terminal's sellable menu is out of date: another
 * terminal marked something unavailable or retired it.
 */
export const MENU_STALE_CODES: ReadonlySet<string> = new Set([
  "MENU_ITEM_UNAVAILABLE",
  "MENU_ITEM_RETIRED",
  "SIZE_UNAVAILABLE",
  "SIZE_RETIRED",
  "MODIFIER_OPTION_UNAVAILABLE",
  "MODIFIER_OPTION_RETIRED",
  "COMMIT_MENU_ITEM_UNAVAILABLE",
  "COMMIT_MENU_ITEM_RETIRED",
  "COMMIT_SIZE_UNAVAILABLE",
  "COMMIT_SIZE_RETIRED",
  "COMMIT_MODIFIER_OPTION_UNAVAILABLE",
  "COMMIT_MODIFIER_OPTION_RETIRED",
  "COMMIT_MODIFIER_GROUP_RETIRED",
]);

export function isMenuStaleError(error: unknown): boolean {
  return error instanceof ApiError && MENU_STALE_CODES.has(error.code);
}
