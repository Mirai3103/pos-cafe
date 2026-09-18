# Define the loyalty extension boundary

Type: grilling
Status: resolved

## Question

Confirm whether customer loyalty is entirely post-launch, then decide what—if anything—the Core POS must preserve now so a later loyalty effort can identify customers and award benefits without coupling anonymous opening-day Orders to an unfinished loyalty model. Include the privacy boundary and explicitly rule premature loyalty behavior out of the MVP.

## Answer

### Opening-day scope

- The **Loyalty Program** is entirely post-launch. The opening-day Core POS has no enrollment, member lookup, reusable customer profile, points calculation or balance, reward redemption, tier, loyalty discount, loyalty reporting, or marketing behavior.
- Every opening-day **Service Session** remains anonymous. Anonymous historical sales are never retroactively assigned to a later Loyalty Account, whether by staff judgment, receipt lookup, Payment details, or inferred identity.
- The MVP does not reserve empty customer or loyalty fields, build an unused association table, publish loyalty-specific events or endpoints, add plugin hooks, or introduce a dormant discount engine.

### Privacy and purpose boundaries

- Core POS does not collect or persist a name, phone number, email address, birth date, or other reusable identity for loyalty purposes.
- A sender name visible while staff confirm a Manual QR Payment is not copied into the Payment record as customer identity.
- Buyer information required by the fiscal-invoice path remains purpose-limited fiscal data and never becomes a Loyalty Account or Loyalty Association without a separate, explicit future consent flow.
- If the Preparation Queue later uses a short pickup label, it is an operational label only: it cannot support customer lookup, purchase-history linkage, or loyalty inference.
- Withdrawal of future loyalty consent does not delete the Core POS's required sales, Payment, Refund, audit, or fiscal history. A future privacy policy may delete or anonymize the Loyalty Account and sever its association, and Core must not use the remaining anonymous history to reconstruct identity.

### Preserved extension boundary

- Core POS preserves domain-neutral, stable identities and durable relationships among Service Sessions, Checks, Completed Sales, Order Items, Payments, Refunds, and corrections.
- It preserves the sales facts a future loyalty effort may consume: completion time, service mode, immutable item names and configurations, quantities and price components, Check totals, and linked post-sale adjustments.
- Core does not own loyalty eligibility, points, account balances, consent, or customer contact data. It records no `customerId`, `loyaltyAccountId`, `eligibleSpend`, `pointsEarned`, reward flag, or similar unfinished loyalty concept.
- A future Loyalty Program owns **Loyalty Accounts**, consent, **Loyalty Associations**, the **Loyalty Ledger**, and reward rules. Its integration may use a query, event, or adapter chosen by that future effort, but it must not depend on direct access to Core's internal storage.

### Attribution, earning, and redemption

- A future Loyalty Association links one explicitly consenting Loyalty Account to one post-launch **Check**, not to an Order, Payment, or entire Service Session. One Check supports one Loyalty Account in the first loyalty version; a party that wants separate attribution must split the Check before Payment under the existing Check rules.
- **Completed Sale** is the source of truth for earning. Loyalty grants benefits only after the Completed Sale containing the associated Check is complete, using the finalized sales facts attributable to that Check.
- A later Refund or correction remains a Core sales fact and causes a corresponding adjustment in the Loyalty Ledger; Core never mutates the Completed Sale or calculates points itself.
- Future redemption introduces an explicitly sourced **Loyalty Benefit** applied to a Check before settlement. It must not masquerade as a Comp, Payment, negative Menu Price, or mutation of an Order Item.

### Deferred to the future loyalty effort

- Identification and enrollment method, consent text and evidence, account recovery, duplicate-account handling, retention policy, and access controls.
- Whether and for how long a post-launch receipt may be claimed after a sale; no such future choice may make pre-launch anonymous sales claimable.
- Earning eligibility, formulas, tiers, expiry, benefit catalog, abuse controls, fiscal/accounting treatment, UI, and the concrete integration mechanism.
