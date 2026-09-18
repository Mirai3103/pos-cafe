# Define payments, corrections, shifts, and reconciliation

Type: grilling
Status: resolved
Blocked by: 01

## Question

What opening-day rules govern cash and Manual QR Payments, change due, payment timing, corrections, voids, refunds, opening and closing a shift, expected versus counted cash, QR totals, discrepancies, and accountability? Decide the smallest auditable settlement model that lets the Core POS replace the existing sales process.

## Answer

### Sales Shift boundary

- A **Sales Shift** is one continuous accountability window for the cashier station's cash fund and payment activity, not an employee login session or calendar day. Only one may be open at a time; opener, closer, and every intervening action retain their own staff identity and time.
- Staff enter the actual counted **Opening Float** when opening. The MVP records one whole-VND total rather than denomination counts and never copies the prior Shift's closing figure as fact.
- An open Sales Shift is required before starting a new Service Session, submitting an Order, receiving a Payment, issuing a Refund, or recording a financial correction. Menu administration may occur outside a Shift.
- Normal closure is blocked while any Service Session, unsettled Check, pending Refund, Awaiting Submission state, or financial correction remains unresolved. A closed Shift is immutable, cannot reopen or accept new actions, and remains inspectable by date and opening/closing time.

### Commitment, Payment, and submission

- A **Payment** is created only after staff confirm money was actually received. A promise, screenshot, pending transfer, or bank record staff cannot see is not a Payment; takeaway waits or uses cash, while dine-in retains a balance.
- Payment is immutable and applies only to charges already in a Check. Multiple partial Cash and Manual QR Payments may settle one Check under the existing no-split-after-Payment rule.
- **Commit** validates an Order Draft, creates immutable **Committed Item** snapshots, and places their charges in a Check without sending preparation work. **Submit** turns those Committed Items into Order Items in an Order without repricing them.
- Dine-in pay-later may Commit and Submit together before Payment. Dine-in pay-first and takeaway use `Commit → Payment → Submit`; takeaway still requires a Settled Check before submission.
- If the paid Committed Items cannot be submitted, they remain visibly **Awaiting Submission**. Staff retry Submit or Cancel and Refund; the system never charges again, silently submits, or permits Service Session or Shift closure while the state remains.
- An unsubmitted checkout with no Payment may terminate as an auditable **Abandoned Checkout** with actor, time, and reason. If any Payment exists it must first be fully refunded; submitted work instead follows Cancellation or Waste. Abandonment creates no Completed Sale, while a Service Session that had a submitted Order closes through the normal Completed Sale lifecycle.

### Cash and Manual QR semantics

- A **Cash Payment** records the amount applied to the Check separately from tendered cash and change due. Applied amount cannot exceed the balance; tendered minus change equals the net cash effect, and cash may pay only part of the Check.
- A **Manual QR Payment** records the exact confirmed bank receipt, confirmation actor and time, and an optional bank transaction reference. It has no cash-style change, and the Core stores neither transfer screenshots nor reusable sender identity.
- Staff ask for no more than the remaining balance. If the customer nevertheless transfers excess, the Core records the full receipt, tracks the excess due back, and requires a Manual QR Refund before the Check can finish. Excess is neither tip nor revenue.
- A Check is **Settled** only at exactly zero balance with no pending Refund or customer excess. Post-sale Refunds and corrections link to the immutable Completed Sale rather than reopening it.

### Corrections and Refunds

- **Payment Void** reverses an incorrect Payment record without representing money returned. While the original Shift is open, staff Void the incorrect record and add a linked correct Payment where necessary; records are never edited or deleted.
- After the original Shift closes, a Manager appends a **Post-Shift Payment Correction**. It links the erroneous record, replacement if any, and unresolved difference without rewriting the closed Shift or pretending that its bookkeeping correction moved money in the current Shift.
- A real current-Shift Payment, Refund, or Cash Movement affects that Shift's reconciliation. A bookkeeping-only post-shift correction does not. If it reveals a shortage, staff resolve it with real money or preserve an auditable discrepancy rather than inventing a balancing transaction.
- Every **Refund** links a valid Cancellation, Comp, or correction to one or more original Payments, cannot exceed the corrected refundable amount or any Payment's remaining refundable amount, and returns through the original method. A voided Payment cannot receive a Refund.
- Cash Refund completes when cash is returned. Manual QR Refund is pending until staff confirm the outbound transfer. A later exceptional Refund belongs to the currently open Shift while linking the Payment and Completed Sale from the earlier Shift.

### Cash movements and reconciliation

- A reasoned **Cash Movement** is either Pay In, such as adding change money, or Pay Out, such as a safe drop. It is not a Sale, Payment, Refund, supplier expense, personal advance, tip jar, or petty cash transaction.
- **Expected Cash** is `Opening Float + Cash Payments − Cash Refunds + Pay Ins − Pay Outs`, including valid same-Shift Payment Void reversals. Cash uses applied amount because tendered cash less change has the same net effect.
- At closure, staff perform a blind cash count before seeing Expected Cash. Recounts are allowed, but initial and final entries retain actor and time.
- The Core separately shows expected Manual QR received and refunded totals. Closing staff compare the bank application and enter observed received/refunded totals, including explicit zeroes; no screenshot or sender data is retained.
- A nonzero **Shift Discrepancy** triggers recount or recheck, a required reason, and Manager Approval, but may then close. The discrepancy remains in the immutable Shift; staff never add fake Payments, Refunds, or Cash Movements to force agreement.
- The closure snapshot preserves opener/closer and times, Opening Float, Payment totals by method, Refunds, Payment Voids, Cash Movements, expected and observed cash/QR, discrepancies, reasons, and acknowledgers. Later Post-Shift Payment Corrections remain append-only links shown alongside rather than rewriting this snapshot.

### Accountability boundary

- Payment Void, Post-Shift Payment Correction, Refund, Cash Movement, Abandoned Checkout, and approval of a nonzero Shift Discrepancy require a reason category and may include a note. Every financial action retains actor, time, method, and amount.
- Exact role permissions, fresh-PIN requirements, and approval rules belong to “Define staff permissions and the audit trail.” Fiscal-document effects of Payment, Refund, and correction belong to “Choose the opening-day fiscal-invoice path.”
- Reporting in this effort stops at the read-only Shift reconciliation record; broader analytics and custom exports remain outside this decision.
