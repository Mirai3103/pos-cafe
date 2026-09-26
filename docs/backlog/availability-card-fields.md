# Follow-up: catalog fields the availability cards mock today

**Status:** completed by backend alignment BA-1 ([spec](../superpowers/specs/2026-09-29-backend-alignment-ba1-catalog-design.md))
**Blocked by:** none
**Source:** Web slice 9a UAT feedback (2026-09-25): the "Kho & Món Tạm Hết" tab must
match `design-system/pos-cafe/pages/settings.html` first; fields the backend lacks are
mocked on the web until this ticket lands.

**What must become true:** every value on a "Kho & Món Tạm Hết" card comes from the
API. Today three of them do not.

| Card field | Today (web) | What the backend must provide |
| --- | --- | --- |
| Thumbnail image | `mockImageUrl()` returns a placeholder-service URL; a failed load falls back to the category icon | The catalog has no image for a Menu Item. It needs one (stored, served, and set by a Manager in slice 9b), and `GET /catalog/menu/availability` must return its URL. |
| Price | `mockPriceVnd()` returns a hard-coded price for the `dev-seed` names; any other item shows "—" | `GET /catalog/menu/availability` omits prices. It must return the card price: the item price, or the smallest Size price for a sized item, and the surcharge for a Modifier Option. The availability capability is held by Barista, who lacks `catalog.view_prices`; the design must decide whether the price is shown to Barista. |
| Item code ("CFSD") | Derived from the name with the POS cafe acronym rule (`getAcronym`) | The catalog has no code. Decide whether Managers set one or the derived acronym is the rule; if stored, return it. |

## Acceptance criteria

- [x] `GET /catalog/menu/availability` returns image URL and price (and code, if stored)
      for Menu Items, and price for Modifier Options, per the decisions above.
- [x] `web/src/features/settings/lib/availability-mock.ts` is deleted, and
      `availability-cards.ts` reads those fields from the response.
- [x] Price visibility for a session without `catalog.view_prices` follows the decision
      recorded in `spec/decisions.md`.

## Already present

- `web/src/features/settings/lib/availability-mock.ts` holds every mocked value, with a
  header comment pointing here. Nothing else in the web reads mock data.
- Prices already exist in the catalog (`menu_items`, `menu_item_sizes`,
  `modifier_options`); only the availability projection omits them.
