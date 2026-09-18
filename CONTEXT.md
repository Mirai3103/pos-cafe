# Cafe POS

Domain language for the sales workflow of this single cafe.

## Language

**Core POS**:
The product boundary covering the cafe's menu, orders, checkout, payment records, and minimum sales reconciliation needed to operate the store.
_Avoid_: Cafe management system, all-in-one F&B platform

**Supported POS Client**:
A browser device on the cafe's staff LAN that uses the authoritative local Core POS service and authenticates each staff member through their own PIN. The cashier station and Preparation Queue display are the primary clients; staff phones reached through the local access QR are supported secondary clients, but no client owns an offline transaction queue.
_Avoid_: Independent offline register, trusted device identity, guest-network access

**Local Access QR**:
A displayed or printed QR code containing only the Core POS's stable staff-LAN URL so an employee can open it on a Supported POS Client; it grants no authority, carries no credential or token, and remains subject to individual PIN authentication.
_Avoid_: Login QR, bearer token, public Internet URL

**POS Operations Owner**:
The designated Manager accountable for daily Core POS readiness, backup evidence, outage handling, recovery reconciliation, and approval of production updates; technical work may be delegated, but this accountability may not be left with the original developer by default.
_Avoid_: Server administrator, developer on call, whoever is on shift

**POS Technical Custodian**:
The named person or support provider authorized to administer and recover the local POS service under the POS Operations Owner's approval, with a documented substitute rather than a single irreplaceable operator.
_Avoid_: POS Operations Owner, any staff member with server access

**Recovery Custody Pack**:
The owner-controlled, sealed and maintained set of recovery instructions, backup locations, access details, and protected key material needed for a replacement technical custodian to recover the Core POS without relying on the original developer's personal accounts or memory.
_Avoid_: Plain-text password sheet, developer-only account, undocumented recovery knowledge

**Outage Sale Record**:
A pre-numbered paper record created when the local POS service or cafe LAN is unavailable, retaining the actual service time, service context, ordered items and choices, amounts, Payment method, fulfillment state, responsible staff, and later corrections needed for recovery reconciliation.
_Avoid_: Order Draft, ordinary receipt, unnumbered note, transaction silently backdated after recovery

**Outage Recovery Entry**:
An auditable reconstruction in the restored Core POS linked one-to-one to an Outage Sale Record and retaining its actual occurrence time, entered under Manager control without recreating already-completed preparation work or pretending the entry occurred while the system was available. It reconciles within the original Sales Shift if that Shift remains open; otherwise it is recorded as a post-Shift correction without reopening the Shift.
_Avoid_: New live Order, duplicate Sale, hidden adjustment

**POS Service Incident**:
The accountable operating interval declared when the local Core POS service or staff LAN has not recovered within five minutes, beginning the numbered-paper fallback and ending only after both primary clients pass a readiness check; it retains the observed start and end, responsible shift staff, known last live transaction, and Outage Sale Record number range.
_Avoid_: Raw server error, momentary reconnect, undocumented downtime

**Daily POS Readiness Check**:
The opening staff member's pre-sales confirmation that the local service and LAN are available, both primary clients can authenticate, system time is correct, the latest expected backup is current, numbered outage forms are ready, and Manual QR Payments can be verified.
_Avoid_: Full restore drill, application test suite, informal glance at the cashier screen

**Staff Identity**:
The durable identity of one individual staff member, authenticated with that person's PIN and retained as the actor of every business action they perform.
_Avoid_: Shared PIN, selected operator name, device identity

**Operational Role**:
A permission set assigned to a Staff Identity; the opening-day roles are Manager, Cashier, and Barista, and one Staff Identity may hold more than one role.
_Avoid_: Job title, device mode, Owner role

**Operational Capability**:
A named business authority granted by a Staff Identity's current Operational Roles; callers and domain modules use the resulting capability union without selecting an active role, while Manager-only administration and Manager Approval still require the Manager role itself.
_Avoid_: Per-person permission override, active role, reusable Manager authority

**Staff Access Session**:
The period during which one Staff Identity is authenticated on one device, ending through sign-out or revocation and remaining independent of a Sales Shift; manual or inactivity lock suspends its authority until the same Staff Identity re-enters their PIN, without ending the session.
_Avoid_: Sales Shift, shared device session, retrospectively selected actor

**Staff Workspace**:
The area of the cafe a Staff Identity declares they are working in for the current Staff Access Session — cashier, manager, or preparation — which the session's inactivity lock policy depends on, since a preparation display tolerates longer idle time than a counter station.
_Avoid_: Operational Role, screen, device mode, active role

**Manager Approval**:
A Manager's fresh-PIN authorization of an exceptional business action at the time it is performed; a Manager may authorize their own action, but another role cannot borrow or reuse that authority.
_Avoid_: Shared manager code, standing approval, two-person control

**Audit Event**:
An append-only record of a meaningful business action or security-relevant access attempt, retaining its actor or attempted identity when known, time, affected business facts, and any required reason or linked correction without recording incidental interface activity.
_Avoid_: Click log, editable history, reconstructed actor

**Reason Category**:
An immutable, operation-appropriate choice from the Core POS's controlled reason catalog; `OTHER` (shown as “Other”) requires a note, and neither the category nor note may be edited after recording.
_Avoid_: Editable comment, arbitrary reason code, interface-only label

**Loyalty Program**:
A post-launch capability that identifies consenting customers and awards or redeems customer benefits; it is not part of the opening-day Core POS.
_Avoid_: Customer identity inside anonymous sales, opening-day discount behavior

**Loyalty Account**:
A future, consent-based customer identity owned by the Loyalty Program rather than inferred from operational labels, fiscal buyer information, or Payment details.
_Avoid_: Anonymous customer record, Customer field on an Order

**Loyalty Association**:
A future, explicitly consented link owned by the Loyalty Program between one Loyalty Account and one post-launch Check identified by the Core POS; it is never inferred or applied to pre-launch anonymous sales.
_Avoid_: Customer ownership on Order, Check, or Payment

**Loyalty Ledger**:
A future record owned by the Loyalty Program of benefits earned from completed sales and adjustments caused by linked Refunds or corrections.
_Avoid_: Points balance on a Completed Sale, mutable sales total

**Loyalty Benefit**:
A future, explicitly sourced benefit applied to a Check before settlement; it is distinct from Comp, Payment, negative Menu Price, or mutation of an Order Item.
_Avoid_: Discount disguised as Comp, loyalty Payment

**In-store Sale**:
A sale initiated by a customer physically present at the cafe, whether they consume the order on premises or take it away.
_Avoid_: Online order, delivery-platform order

**Preparation Queue**:
The shared FIFO view of Preparation Units created when Orders are submitted; staff may work out of order, but only a Remake carries explicit launch-day priority.
_Avoid_: Kitchen printer, kitchen display system

**Preparation Unit**:
One physical unit of an Order Item tracked independently through preparation, even when identical units are grouped for display; it is created when its Order is submitted and is never exclusively assigned to one barista.
_Avoid_: Order Item, claimed job, queue row

**Queued**:
The state of a Preparation Unit that has entered the Preparation Queue but has not explicitly been started.
_Avoid_: Accepted, viewed, assigned

**In Preparation**:
The state of a Preparation Unit after an identified staff member explicitly starts it and before it becomes Ready or Wasted.
_Avoid_: Viewed, claimed, implicitly started

**Ready**:
The state of a Preparation Unit whose preparation is complete but which has not yet been handed to the customer or table.
_Avoid_: Fulfilled, completed Order

**Fulfilled**:
The terminal state of a Preparation Unit after staff confirm it has been handed to the customer or table; an Order's fulfillment is derived from its units rather than tracked independently.
_Avoid_: Ready, Settled Check, Completed Sale

**Cancelled**:
The terminal state of a Preparation Unit withdrawn before preparation begins; its linked Cancellation preserves the commercial withdrawal, actor, time, and reason.
_Avoid_: Deleted, Wasted, silently removed

**Wasted**:
The terminal state of a Preparation Unit that cannot be delivered after preparation has begun; its linked Waste preserves why the prepared work was lost.
_Avoid_: Cancelled, deleted, fulfilled

**Service Number**:
A short operational label for one Anonymous Service Session, used to identify takeaway handoff and shown alongside current Table assignments for dine-in without identifying the customer.
_Avoid_: Customer number, Loyalty Account, Order number

**Preparation Alert**:
A post-submission Cancellation or change notice that remains prominent until an identified staff member acknowledges seeing it; acknowledgment has no commercial or financial effect.
_Avoid_: Order edit, automatic Cancellation acceptance

**Preparation State Correction**:
An auditable declaration that a recorded preparation transition was operationally mistaken, preserving the original action while restoring the Preparation Unit's correct current state.
_Avoid_: Undo, deleted history, silent state reversal

**Manual QR Payment**:
A bank-transfer Payment for the exact amount staff confirm as received in the bank account, without cash-style change or automatic bank or payment-gateway confirmation.
_Avoid_: Integrated QR payment

**Modifier Group**:
A reusable set of menu choices assigned unchanged as a Menu Category default or directly to a Menu Item. Its minimum and maximum count distinct Modifier Options, each selectable at most once; it may propose an explicit valid default set, and an item may exclude an inherited group but cannot redefine it.
_Avoid_: Item-specific topping list

**Menu Category**:
The single operational grouping to which a Menu Item belongs; it may provide default Modifier Groups for every item in that category.
_Avoid_: Tag, multiple-category classification

**Menu Item**:
A product the cafe offers for sale, belonging to one Menu Category and priced in positive whole VND either directly or through a required Size choice.
_Avoid_: Order item, SKU

**Menu Price**:
The final customer-facing whole-VND amount for a configured Menu Item: its direct or Size price plus selected Modifier Option surcharges, identical for dine-in and takeaway.
_Avoid_: Pre-tax price, open price, checkout surcharge, ad hoc discount

**Size**:
An optional dimension of a Menu Item whose configured choices each carry a positive whole-VND selling price; when an item has Size choices, exactly one is required.
_Avoid_: Modifier, hidden standard size, size surcharge

**Modifier Option**:
A selectable preparation choice within a Modifier Group, such as sugar level, ice level, milk choice, or a priced add-on; any price adjustment is a non-negative whole-VND amount.
_Avoid_: Size, discount, Comp

**Availability**:
The temporary eligibility of a Menu Item, Size, or Modifier Option for new work; an item is not sellable while any required Modifier Group has no valid available selection.
_Avoid_: Inventory, deletion, retirement

**Retirement**:
The permanent removal of a menu entity from future selection while preserving its identity and every historical use.
_Avoid_: Temporary unavailability, deletion

**Preparation Note**:
Free text attached to an ordered Menu Item solely as a preparation instruction; it cannot satisfy Modifier Group rules, alter price, or substitute for an unavailable or priced Modifier Option.
_Avoid_: Custom Modifier Option, discount instruction

**Commit**:
The commercial boundary that revalidates an Order Draft, creates immutable Committed Items, and places their charges in a Check without creating an Order or preparation work.
_Avoid_: Submit, Payment, draft save

**Submit**:
The preparation boundary that turns selected Committed Items into an Order and Preparation Units without repricing them.
_Avoid_: Commit, Payment, draft save

**Order Item**:
A Committed Item after submission as part of an Order, retaining its immutable commercial snapshot for preparation and history.
_Avoid_: Current Menu Item, mutable draft item

**Committed Item**:
An immutable commercial snapshot created when a valid draft item's charge joins a Check, preserving names, choices, quantity, price components, total, and Preparation Note before Payment or Order submission.
_Avoid_: Order Draft item, repriced Order Item

**Service Session**:
The continuous period in which one customer party is served, containing its Orders and current Table assignments until staff explicitly close it.
_Avoid_: Order, bill, table booking

**Anonymous Service Session**:
An opening-day Service Session with no durable customer identity or reusable customer profile; operational labels and purpose-limited fiscal buyer information do not turn it into a loyalty relationship.
_Avoid_: Guest customer account, inferred member

**Order**:
A confirmed batch of menu items sent together to preparation within a Service Session.
_Avoid_: Service Session, bill, payment, sale

**Order Draft**:
A mutable proposed batch of menu items that has not been submitted to preparation; submission turns it into an Order.
_Avoid_: Order, cart after submission

**Check**:
A grouping of charges awaiting settlement within a Service Session; Payments settle the Check, while receipts and fiscal invoices are separate documents derived from it.
_Avoid_: Bill, receipt, fiscal invoice, Order

**Payment**:
A confirmed, immutable receipt of money applied to a Check, recorded independently from ordering and preparation; an attempted or unverified transfer is not a Payment.
_Avoid_: Check, receipt, Order

**Cash Payment**:
A Payment recording the amount applied to a Check separately from cash tendered and change due; its net cash effect is the applied amount.
_Avoid_: Cash tender, cash drawer balance

**Payment Void**:
An auditable bookkeeping reversal of a Payment record that did not correctly represent the received money, followed where necessary by a linked correct Payment and never by deletion or mutation.
_Avoid_: Refund, Payment edit, cash return

**Post-Shift Payment Correction**:
An append-only correction recorded after a Payment's Sales Shift has closed, preserving that Shift's reconciliation while linking the erroneous record, its replacement if any, and the resulting unresolved difference.
_Avoid_: Reopening a Shift, rewriting a Payment, current-Shift cash movement

**Sales Shift**:
The continuous accountability window for the cashier station's cash fund and payment activity, opened and closed by identified staff while every intervening action retains its own actor.
_Avoid_: Staff login session, calendar day, employee work schedule

**Opening Float**:
The actual whole-VND cash counted in the cashier station's fund when identified staff open a Sales Shift.
_Avoid_: Expected cash, prior Shift closing balance

**Cash Movement**:
An auditable Pay In or Pay Out that changes a Sales Shift's expected cash without representing a Sale, Payment, or Refund.
_Avoid_: Supplier expense, personal advance, petty cash, hidden drawer adjustment

**Expected Cash**:
The Sales Shift's calculated cash responsibility: Opening Float plus Cash Payments and Pay Ins, less Cash Refunds and Pay Outs, including valid same-Shift reversals.
_Avoid_: Counted cash, target cash, bank balance

**Shift Reconciliation**:
The comparison of a Sales Shift's calculated cash and Manual QR activity with independently counted cash and bank-observed totals before closure.
_Avoid_: Editing transactions to match, sales report

**Shift Discrepancy**:
The preserved difference between a Shift Reconciliation's expected and observed amount after recount or recheck.
_Avoid_: Cash Movement, balancing Payment, hidden correction

**Settled Check**:
A Check whose valid Payments leave exactly zero balance with no pending Refund or customer excess; it accepts no further merge or split operations.
_Avoid_: Completed Sale, closed Service Session

**Abandoned Checkout**:
The recorded terminal outcome of unsubmitted Committed Items and their Check after the customer leaves, preserving actor, time, and reason without creating a Completed Sale.
_Avoid_: Completed Sale, Cancellation, deleted draft

**Awaiting Submission**:
The unresolved state of paid Committed Items that have not yet entered an Order and the Preparation Queue.
_Avoid_: Submitted Order, automatic retry, second Payment

**Cancellation**:
The recorded withdrawal of all or part of a submitted Order before preparation begins, removing the corresponding charge but preserving who acted and why.
_Avoid_: Deletion, Waste, Refund

**Waste**:
A submitted item that cannot be delivered after preparation has begun, retained as part of the service history.
_Avoid_: Cancellation, deletion

**Comp**:
An explicit waiver of a charge for a Cancellation or Waste, with actor and reason retained.
_Avoid_: Discount, deletion, Refund

**Remake**:
A replacement item linked to a Wasted item and prepared without charging the customer again.
_Avoid_: New sale, duplicate Order item

**Refund**:
A recorded return of money through the original Payment method, allocated to that Payment and its Check without exceeding the valid corrected amount or changing the original financial history.
_Avoid_: Negative Payment, deleted Payment, Comp

**Table**:
A named physical service location that may be associated with one or more active Service Sessions.
_Avoid_: Order owner, exclusive tab

**Completed Sale**:
The immutable outcome of a Service Session that staff close after every Preparation Unit, required Remake, Check, Refund, submission, and correction has reached its permitted terminal state.
_Avoid_: Paid Order, receipt, open tab
