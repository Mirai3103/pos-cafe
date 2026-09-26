# Backend alignment: close the gaps the web slices recorded

**Status:** in-progress (BA-1 done; BA-2–BA-5 ready-for-design)
**Blocked by:** none
**Source:** Operator decision (2026-09-25), before web slice 9b: align the backend with the
prototype in `design-system/pos-cafe/` before the next screens are built, instead of
cutting each missing feature slice by slice.

**What must become true:** every prototype element that web slices 3 to 9a cut because
the backend lacked it is either served by the API or assigned to a named owner (a
roadmap phase or an open question). Nothing stays cut without an owner.

## How the gaps were sorted

Every recorded cut in the web slice specs (slices 2 to 9a) and every prototype page was
checked against the Go routes. A cut falls in one of three groups.

**Backend already serves it; the web has not built it yet.** Not part of this epic.
The later web slices pick them up: Manual QR payment, partial payment, check split and
merge, check target, payment void, refunds, bulk state correction, closed Shift
browsing, and staff administration (`/staff/*` is complete).

**Owned elsewhere.** Not part of this epic, and not duplicated here.

| Gap | Owner |
| --- | --- |
| Cancel an abandoned Service Session (slice 5) | Phase 08, [recover a failed or abandoned checkout](phase-08-recover-failed-or-abandoned-checkout.md) |
| List and search Completed Sales, role-scoped history (slice 8). Only `GET /sales/completed-sales/{id}` exists | Phase 09, [post-Shift corrections and Audit history](phase-09-post-shift-corrections-and-audit.md) |
| K80 receipt, Z-report printing, label reprint | [Receipt and PDF boundary](open-questions.md) |
| Realtime push to KDS and POS | Non-goal of the slice sequence; polling stays |

**Backend gap with no owner.** This epic. Five sub-projects, in delivery order. Each
gets its own design spec, pull request, and UAT gate.

## Sub-projects

| # | Sub-project | What must become true | Unblocks |
| --- | --- | --- | --- |
| **BA-1** | Catalog | A Manager can set the display fields the prototype shows: item image, code, badge, and description; category icon and display order. The structural edits the prototype's forms make are possible: detach a modifier group from an item or category, lift an inherited-group exclusion, add a Size to an existing item, add an Option to an existing group, change a group's selection rule, and move an item to another category. The availability and sellable projections return the fields their screens draw. | Web slice 9b; supersedes [availability-card-fields](availability-card-fields.md); images on the POS menu grid |
| **BA-2** | Store profile and VietQR | A Manager can store the cafe's profile and its VietQR beneficiary, and a client can read them. | Settings tab "Thông tin Quán & VietQR"; the VietQR tab on the POS payment sheet |
| **BA-3** | Preparation station | A category can name the station that prepares it (bar, bakery), and the Preparation Queue can be read per station. KDS "Báo thiếu" reuses the slice 9a availability batch and needs no new backend. | KDS station filter |
| **BA-4** | Tables and floor | Tables can be grouped into zones or floors; a dine-in Session can record a guest count; a Table can carry a "needs cleaning" status. | Tables screen |
| **BA-5** | Service Session operations | Staff can discard an Order Draft in one action ("Hủy đơn"), merge two Tables' Sessions ("Gộp bàn"), and move Committed Items to another Table's Session ("Tách món"). These touch Checks and money, so this sub-project needs the most domain design. | POS "Hủy đơn"; Tables "Gộp bàn" and "Tách món" |

The order puts what unblocks the most, at the lowest domain risk, first. BA-1 blocks
web slice 9b directly. BA-5 goes last because it needs the most domain work.

BA-1 done: UAT passed 2026-09-26; implemented on branch backend-alignment-ba1 and merged into master.

## Acceptance criteria

- [ ] BA-1 through BA-5 each have an approved design spec, a merged pull request, and a
      passed UAT gate.
- [ ] Every web cut that a sub-project resolves is removed from the web (disabled
      buttons enabled, mocks deleted).
- [x] `availability-card-fields.md` is closed by BA-1.

## Already present

- Availability commands and the atomic batch (ADR-055) are live; "Báo thiếu" on the
  KDS can call them.
- Prices already exist in the catalog; only the availability projection omits them.
- Tables already store `name` and `available`.
