# Define the Preparation Queue workflow

Type: grilling
Status: resolved
Blocked by: 01, 02

## Question

When and how does accepted work enter the Preparation Queue, which item or Order states do baristas need, how are changes and cancellations communicated after preparation begins, how is ready work identified for table service versus takeaway, and what ordering or urgency rules are essential at launch? Decide behavior, not screen layout.

## Answer

### Queue entry and unit of work

- Submitting an Order atomically creates one **Preparation Unit** for each physical unit represented by its Order Items. Identical units may be grouped for display, but each retains independent preparation state so partial Cancellation, Waste, Remake, and fulfillment remain precise.
- Every submitted Order Item enters preparation; there is no separate barista acceptance step and viewing the queue has no lifecycle effect.
- The Preparation Queue is shared. A Preparation Unit is never exclusively claimed by one barista, although every action retains its actor.

### Preparation lifecycle

- The normal lifecycle is `Queued → In Preparation → Ready → Fulfilled`.
- Starting preparation is an explicit staff action. It is the boundary after which an undeliverable unit follows the existing Waste rules rather than Cancellation.
- A Queued unit may become `Cancelled`. An In Preparation or Ready unit may become `Wasted`. A Remake is a new, linked Preparation Unit with its own lifecycle, not a state applied to the Wasted unit.
- Preparation state belongs to individual Preparation Units. Any summary for an Order is derived from its units and is never an independently editable state.
- Units may reach Ready and Fulfilled independently, allowing partial handoff. Bulk actions are permitted but apply and record their result per selected unit; an invalid unit remains unchanged without corrupting valid units.

### Identification and handoff

- Every Anonymous Service Session has a short **Service Number**. Takeaway handoff uses this number; dine-in work shows it alongside the Service Session's current Table assignments, preserving identification when Tables are shared or changed.
- Customer name or phone number is not required for preparation or handoff.
- `Ready` means preparation is complete but handoff has not been confirmed. `Fulfilled` is recorded only when staff confirm the unit was handed to the customer or Table.

### Changes, cancellations, and mistakes

- A submitted item is never edited in place. A requested change Cancels the old Queued unit and submits the intended configuration through a new Order; once work has started, the existing Waste, Comp, Refund, and Remake rules apply.
- A post-submission Cancellation or change creates a prominent **Preparation Alert** containing the affected snapshot and correction meaning. It remains active until an identified staff member acknowledges seeing it. Acknowledgment records actor and time but has no commercial or financial effect.
- An accidental preparation transition is corrected through an auditable **Preparation State Correction** that preserves the original action, records actor, time, reason, erroneous state, and corrected state, and restores the correct operational state. The staff-permissions decision owns who may perform or approve it.

### Ordering and active visibility

- The queue's canonical order is FIFO by Order submission time across takeaway and dine-in. Staff may work out of order without rewriting that ordering.
- Only a Remake carries explicit launch-day priority. The MVP has no general rush flag, promised preparation time, or SLA. It shows elapsed Queued time and elapsed In Preparation time so staff can judge aging work.
- Fulfilled units leave the active queue immediately. Cancelled and Wasted units leave only after their Preparation Alert is acknowledged. All terminal units remain in the Service Session history and audit trail.
- Exact screen layout belongs to the workflow prototype; role permissions and detailed audit authorization belong to the staff-permissions decision.
