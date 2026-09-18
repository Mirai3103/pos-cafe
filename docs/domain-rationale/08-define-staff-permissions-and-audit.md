# Define staff permissions and the audit trail

Type: grilling
Status: resolved
Blocked by: 01, 06, 07

## Question

What may Manager, Cashier, and Barista Operational Roles view or change; which actions require a fresh PIN or Manager Approval; and which business events must retain actor, time, reason, and before/after meaning? Keep the control model proportionate to one cafe while making corrections and reconciliation trustworthy.

## Answer

### Identity, roles, and device access

- Every person uses one durable **Staff Identity** and an individual PIN. Shared PINs, retrospectively selecting another actor, and treating a device as the actor are forbidden.
- The opening-day **Operational Roles** are Manager, Cashier, and Barista. Owner uses Manager authority rather than a separate operational role. A Staff Identity may hold multiple roles and receives their combined capabilities without selecting an active role; audit records the identity and capability actually used.
- One Staff Identity is active on a device at a time. Staff may log out and log in freely on the same device. The cashier station locks after five minutes of inactivity and the Preparation Queue display after fifteen minutes; unlocking requires that staff member's PIN.
- Business work is not owned by a Staff Access Session. Another authorized person may log in and continue an Order Draft, Check, Service Session, or Preparation Unit, while each action retains its own actor.

### Routine permissions and visibility

- **Barista** may view Service Number, current Table assignments, item snapshots, modifiers, Preparation Notes, Preparation Alerts, and preparation history. Barista may make normal Preparation Unit transitions, acknowledge Alerts, record Waste with a reason, and create a reasoned Remake linked to the Wasted unit. Barista cannot view Payments, Shift money, or financial audit history and cannot change Checks, prices, or charges.
- **Cashier** may create and operate Service Sessions, Order Drafts, Orders, and Checks; Commit and Submit valid work; receive Cash and Manual QR Payments; confirm fulfillment; perform valid pre-Payment Check split or merge; Cancel Queued work with a reason; record an Abandoned Checkout; and open or normally close a Sales Shift. Cashier may view the complete service, settlement, and current-Shift history needed to perform those duties, but not staff administration or the complete audit history.
- Cashier and Barista may change Availability for immediate operations; the Audit Event retains actor, time, and before/after value, without requiring a fresh PIN.
- **Manager** receives all Cashier and Barista capabilities, may inspect the complete business and audit history, and exclusively controls prices, Size and Modifier configuration, menu structure, Retirement, Staff Identity state, PIN reset, and Operational Role assignment.
- A Manager may manage their own identity, but the last enabled Staff Identity with Manager authority cannot be disabled or stripped of that authority. Staff administration and price changes require the Manager to re-enter their PIN; ordinary menu configuration does not require re-entry when a Manager is already authenticated.

### Corrections and Manager Approval

- A Preparation State Correction requires the correcting staff member to re-enter their own PIN and give a reason, but does not require Manager Approval.
- Comp, Refund, Payment Void, Cash Movement, Post-Shift Payment Correction, and closure with a nonzero Shift Discrepancy require **Manager Approval**. Cashier may prepare the proposed action, but it has no effect until approved.
- One Manager must be designated and present at the cafe for every open Sales Shift. An unavailable approval waits; staff may not bypass authority or communicate a reusable Manager PIN.
- Approval requires a fresh Manager PIN and authorizes exactly one presented action, including its type, target, amount where applicable, and reason. It creates no Manager mode or grace period. A Manager may approve their own action, in which case initiator and approver are the same Staff Identity; the MVP does not require two-person control.

### Audit coverage and reasons

- An append-only **Audit Event** records meaningful business events rather than interface clicks. Coverage includes authentication and Staff Access Session changes; Shift opening, counting, reconciliation, and closure; Commit, Submit, Cancellation, Abandoned Checkout, Check split or merge; every Preparation Unit transition, Alert acknowledgment, Waste, Remake, and state correction; every Payment, Refund, Payment Void, Cash Movement, Post-Shift Payment Correction, Comp, and discrepancy approval; Service Session closure and Completed Sale creation; every price, Availability, menu, and Retirement change; and every Staff Identity, role, and PIN-reset administration event.
- Each event retains event type, actor, time, affected business identity, and its business meaning. Manager Approval additionally retains initiator and approver. Authentication failures are security events, without exposing the attempted PIN.
- A fixed **Reason Category** is required for post-Submit Cancellation, Abandoned Checkout, Waste, Remake, Preparation State Correction, Comp, Refund, Payment Void, Cash Movement, Post-Shift Payment Correction, nonzero-discrepancy approval, and Retirement. A note is optional except for `OTHER` (shown as “Other”), when it is mandatory. Recorded categories and notes cannot be edited. Routine Payment, Submit, Ready, and Fulfilled actions require no reason.
- The opening-day catalog is operation-scoped. Interfaces render Vietnamese labels, while records and APIs retain the stable codes below; a command rejects a category outside its operation's list.

| Operation                          | Allowed Reason Category codes                                                             |
| ---------------------------------- | ----------------------------------------------------------------------------------------- |
| Cancellation                       | `CUSTOMER_REQUEST`, `ORDER_ENTRY_ERROR`, `ITEM_UNAVAILABLE`, `OTHER`                      |
| Abandoned Checkout                 | `CUSTOMER_LEFT`, `CUSTOMER_REQUEST`, `SYSTEM_FAILURE`, `OTHER`                            |
| Waste                              | `PREPARATION_ERROR`, `QUALITY_FAILURE`, `CUSTOMER_REQUEST`, `OTHER`                       |
| Remake                             | `PREPARATION_ERROR`, `QUALITY_FAILURE`, `OTHER`                                           |
| Preparation State Correction       | `STATE_RECORDED_IN_ERROR`, `OTHER`                                                        |
| Comp                               | `CAFE_ERROR`, `QUALITY_FAILURE`, `SERVICE_RECOVERY`, `OTHER`                              |
| Refund                             | `CUSTOMER_REQUEST`, `ITEM_UNAVAILABLE`, `CAFE_ERROR`, `RETURN_QR_EXCESS`, `OTHER`         |
| Payment Void                       | `DUPLICATE_PAYMENT`, `WRONG_AMOUNT`, `WRONG_METHOD`, `PAYMENT_RECORDED_IN_ERROR`, `OTHER` |
| Cash Movement                      | `ADD_CHANGE_FUND`, `REMOVE_EXCESS_FLOAT`, `SAFE_DROP`, `OTHER`                            |
| Post-Shift Payment Correction      | `DUPLICATE_PAYMENT`, `WRONG_AMOUNT`, `WRONG_METHOD`, `PAYMENT_RECORDED_IN_ERROR`, `OTHER` |
| Nonzero Shift Discrepancy approval | `CASH_COUNT_DIFFERENCE`, `QR_OBSERVATION_DIFFERENCE`, `UNEXPLAINED`, `OTHER`              |
| Retirement                         | `NO_LONGER_OFFERED`, `MENU_RESTRUCTURE`, `OTHER`                                          |

- Price, Availability, menu configuration, Staff Identity state, and Operational Role changes retain before/after facts. Preparation State Correction retains the erroneous and corrected state. PIN values never appear in before/after data.
- Immutable financial and correction records are not represented as edits: Refund, Comp, Payment Void, and Post-Shift Payment Correction link to their source records and state their new effect. Bulk actions retain the common request and a separate success or failure result for every target.
- Audit Events cannot be edited or deleted through the Core POS and remain available for at least as long as the business records they evidence. Manager may view the complete audit history; Cashier and Barista receive only the operational history allowed by their duties.
