# Define the mixed-service Order lifecycle

Type: grilling
Status: resolved

## Question

What canonical Order lifecycle supports counter takeaway plus table-service dine-in, including table identity, submission to preparation, optional payment before or after preparation, completion, edits, cancellation, refunds, split or partial settlement, and the boundary between an Order and a completed Sale? Resolve the vocabulary in `CONTEXT.md` and keep the rules small enough for opening-day operation.

## Answer

### Domain boundaries

- A **Service Session** is the complete period of service for one customer party. It owns the party's Orders and Checks and remains active until staff explicitly close it.
- An **Order Draft** is freely editable. Submitting it creates an **Order**, a confirmed batch sent to the Preparation Queue.
- A **Check** groups charges awaiting settlement. A **Payment** applies to a Check. Receipt PDFs and fiscal invoices are separate documents derived from the settled business record.
- A **Completed Sale** is created only when an eligible Service Session is explicitly closed; it is immutable.

### Service mode and tables

- Staff enter table orders directly through a portable browser client rather than transcribing paper notes.
- Service Sessions and Tables are many-to-many: a party may occupy several Tables and multiple independent parties may share a Table.
- Shared occupancy warns but does not block. Moving a party changes the Service Session's Table assignments, preserving prior history.
- Takeaway Service Sessions have no Table.

### Ordering and payment timing

- Dine-in Orders may be submitted before or after Payment.
- Takeaway must be fully paid before its Order is submitted to preparation.
- A Service Session may contain multiple Orders and multiple Checks.
- A new Order joins the current unpaid Check by default. It creates a new Check when the previous Check is Settled or staff intentionally start a separate Check.
- Reaching zero balance does not close the Service Session. A later Order can create a new balance until staff explicitly close the session.

### Check operations

- Checks may be merged only when they belong to the same Service Session, have no Payments, and are not Settled.
- Before Payment, staff may split a Check by item or quantity. Arbitrary proportional or equal-value splitting is outside the opening-day lifecycle.
- Once a Check has any Payment, it cannot be merged or split.
- A Check may receive multiple Cash and Manual QR Payments. Cash records tender and change; Manual QR records the amount staff confirm as received.
- A Check is Settled only when valid Payments fully cover its amount.

### Corrections before and during preparation

- Before submission, staff may edit or delete an Order Draft freely.
- Before preparation begins, all or part of a submitted Order may be **Cancelled**. The Cancellation records actor and reason, notifies the Preparation Queue, and removes the corresponding charge.
- Cancellation of an already-paid charge requires a Refund; it never deletes the original Payment.
- Once preparation begins, an undeliverable item becomes **Waste** rather than disappearing from history.
- Customer-requested Waste keeps its charge by default. A Manager may approve a **Comp** with a recorded reason.
- A cafe-caused replacement is a **Remake** linked to the Wasted item and does not charge the customer again.
- Exact role permissions for Cancellation, Waste, Comp, Remake, and Refund belong to the staff-permissions decision.

### Refund and closure

- A **Refund** is a distinct outflow linked to the original Payment, Check, and Cancellation or Comp. It may be partial or full and never mutates the original Payment.
- Cash Refund completes when money is returned. Manual QR Refund completes only after staff confirm the outbound transfer.
- A Service Session may close only when every Order item is terminal (`Fulfilled`, `Cancelled`, or `Wasted`), required Remakes are complete, every Check is Settled, no Refund is pending, and staff explicitly invoke Close.
- Close releases current Table assignments and creates the immutable Completed Sale.
- An unsubmitted checkout instead follows the terminal exception defined by [Define payments, corrections, shifts, and reconciliation](06-define-payments-shifts-and-reconciliation.md); that decision owns the detail.
- A Completed Sale never reopens. Later corrections and Refunds link back to it; a new purchase after Close starts a new Service Session even if the party remains at the same Table.
- Fiscal-document correction behavior is deferred to the Vietnam fiscal-obligations research.
