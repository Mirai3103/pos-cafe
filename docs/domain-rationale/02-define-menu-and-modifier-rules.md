# Define the menu and Modifier Group rules

Type: grilling
Status: resolved

## Question

What are the canonical rules for menu categories, items, sizes, reusable Modifier Groups, min/max selection, price adjustments, sugar/ice choices, free-text notes, availability, price changes, and preservation of historical Order meaning? Decide which combinations the Core POS must accept or reject without designing storage or UI yet.

## Answer

### Catalog structure

- Each **Menu Item** belongs to exactly one **Menu Category**. Category names are unique across the menu; item names are unique within their category, comparing without case or surrounding whitespace.
- An unsized Menu Item carries one direct positive whole-VND price. A sized item has no independent base price and requires exactly one **Size**, each with its own absolute positive whole-VND selling price. Size names are unique within the item.
- A configured Menu Item has the same price for dine-in and takeaway. A genuinely different product or service-specific offer must be a separately named Menu Item.
- The MVP has no open-priced item, ad hoc line-price override, checkout surcharge, service charge, coupon, percentage discount, promotional-combo engine, or scheduled price version. The menu price is the final customer-facing amount; later fiscal-invoice rules may decompose tax without changing that committed total.

### Modifier Groups and choices

- Size is separate from modifiers. Sugar, ice, milk choice, and priced add-ons all use **Modifier Groups** containing **Modifier Options**.
- A Menu Category may supply Modifier Groups as defaults to all its items. A Menu Item may exclude an inherited group or attach additional groups, but it cannot redefine an inherited group's choices, constraints, defaults, or prices. A group reached through both category and item attachment applies only once; a materially different rule requires a distinct group.
- A group defines finite minimum and maximum counts over distinct selected options. Each option may be selected at most once; repeated-option quantities are outside the MVP. Explicit choices such as “2 extra shots” may represent a supported quantity.
- A group may define an explicit default selection set, which must itself satisfy the group's rules. Defaults appear as ordinary selections in the Order Draft; no hidden modifier is added during submission.
- Modifier Group names are unique across the menu, and Modifier Option names are unique within their group, comparing without case or surrounding whitespace.
- Modifier Options carry non-negative whole-VND adjustments. An item's total is its direct or selected Size price plus all selected adjustments. A reusable option's adjustment does not vary by item; a different adjustment requires a distinct group.
- Groups act independently. The MVP has no conditional compatibility rules between Size and modifiers or between groups. Different valid combinations require separately configured Menu Items or Modifier Groups.

### Notes and quantities

- A **Preparation Note** is an item-level preparation instruction only. It cannot satisfy group minimums, alter price, request an unavailable option, or substitute for a priced add-on.
- One Order Item may have quantity greater than one only when Menu Item, Size, selected Modifier Options, Preparation Note, and unit-price composition are identical. Any difference creates a separate Order Item so preparation, correction, and Check splitting retain precise meaning.

### Availability and catalog lifecycle

- **Availability** may temporarily disable a Menu Item, Size, or Modifier Option for new work. Category and Modifier Group do not need their own availability state.
- A Menu Item is not sellable when unavailable or when any required group has no valid combination of available options.
- **Retirement** permanently removes a Menu Item, Size, Modifier Group, or Modifier Option from future selection without deleting its identity or historical use. Renames and price changes may retain identity; a material change in what a product means requires a new Menu Item.
- Manager menu changes take effect immediately for charges not yet committed. Detailed authorization, reason capture, and before/after audit rules belong to “Define staff permissions and the audit trail.”

### Validation and historical meaning

- An Order Draft may be temporarily incomplete. Before its charge joins a Check, the Core POS rejects a selection if the item is unavailable or retired; a required Size is absent, invalid, unavailable, or retired; group min/max is violated; an option is duplicated, outside the applicable group, unavailable, or retired; or a required group has no valid available selection.
- The charge joining a Check is the commitment point for menu meaning. The later [Define payments, corrections, shifts, and reconciliation](06-define-payments-shifts-and-reconciliation.md) decision owns the Commit, Payment, and Submit sequencing; earlier drafts use the latest catalog, and later menu changes never reprice or invalidate committed work.
- Commitment creates an immutable **Order Item** snapshot containing the historical Category, Menu Item, Size, Modifier Group, and Modifier Option names; quantity; direct or Size price; individual adjustments; total; and Preparation Note. It may retain references to current catalog identities, but historical displays and calculations use the snapshot.
- If availability changes or an ingredient problem appears after commitment, staff use the already-decided Cancellation, Waste, Comp, Remake, or Refund lifecycle rather than mutating the Order Item or revalidating it against the current menu.
