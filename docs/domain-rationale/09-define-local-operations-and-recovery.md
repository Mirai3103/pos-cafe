# Define local operation, backup, recovery, and updates

Type: grilling
Status: resolved
Blocked by: 04

## Question

Given the researched TanStack Start production constraints, what deployment ownership, LAN behavior, startup supervision, backup frequency and destinations, acceptable data-loss window, restore procedure, update cadence, rollback behavior, and manual outage procedure are required for the cafe to rely on one local server from opening day? Resolve operational targets before choosing mechanisms.

## Answer

### Operational ownership

- A designated Manager is the **POS Operations Owner**, accountable for daily readiness, backup evidence, outage handling, recovery reconciliation, and approval of production updates.
- The owner-developer is the opening-day **POS Technical Custodian** and performs administration, recovery, and on-site production updates after the cafe closes. Each responsibility must have a documented substitute.
- The cafe owner controls a sealed, maintained **Recovery Custody Pack** containing recovery instructions, backup locations, protected access details, and key material sufficient for a replacement technical custodian. Recovery must not depend only on the original developer's personal accounts or memory.

### Local service and clients

- Loss of Internet must not interrupt core sales while the local server and staff LAN remain available. The Core POS is not exposed for remote access outside the cafe LAN, and guest Wi-Fi cannot reach it.
- The cashier station and Preparation Queue display are the primary Supported POS Clients. Staff phones are supported secondary clients: an employee joins the staff Wi-Fi, scans a displayed or printed **Local Access QR**, and authenticates with their individual PIN. The QR contains only a stable internal URL and grants no authority.
- Every client uses the one authoritative local service. No browser or phone maintains an independent offline transaction queue. Loss of the server or staff LAN therefore moves the whole cafe to the paper fallback instead of allowing divergent client records.
- The local service starts automatically with its host, restarts after a process failure, becomes usable within five minutes without terminal work, and presents an intelligible failure alert to the POS Operations Owner when automatic recovery fails.
- The server and required LAN equipment must remain available through at least fifteen minutes of power loss, enough to bridge a short interruption or shut down safely. A longer interruption uses the paper fallback.

### Backup and recovery targets

- During operation, backups must support no more than fifteen minutes of data loss after ordinary server or server-disk failure. A backup on the same physical disk does not count; a physically separate local destination provides rapid recovery.
- An encrypted backup outside the cafe is updated at least daily. Loss of the whole site may therefore lose at most twenty-four hours of data. When Internet is available, off-site transfer may run more frequently, but transfer failure queues work and never stops sales.
- Recovery coverage includes authoritative business data, required runtime configuration, and application-version identity. Secrets may not be retained in readable form in ordinary backup contents.
- Retain at least seven recent local recovery points, thirty-five daily off-site backups, and twelve off-site month-end backups. This operational backup policy does not replace any longer legal retention obligation later established for fiscal records.
- Backup integrity is checked daily. A full restore to an isolated environment is completed before opening, after a material data-format change, and at least quarterly. The POS Technical Custodian performs the restore; the POS Operations Owner verifies login, business history, Shift reconciliation, and a safe test transaction. Evidence retains the backup used, date, elapsed recovery time, and result.
- Replacing a failed host and restoring service must take no more than four hours and, in all cases, complete within the same business day.

### Updates and rollback

- Production never updates silently or automatically. The POS Technical Custodian prepares and performs an on-site update after closing; the POS Operations Owner approves its maintenance window. Normal updates use a planned monthly window, while a security issue or sale-blocking defect may justify an earlier window.
- No update is made during the fourteen days before opening unless the current version itself prevents go-live readiness.
- Every update starts with a verified pre-update backup and a recoverable copy of the preceding application version. Before another Sales Shift may open, checks must cover authentication, menu access, Order submission, Preparation Queue delivery, Payment, Shift reconciliation, and PDF generation.
- Failure of any essential check requires rollback during the same night. An incompatible or partially applied data migration may not be left for the next opening; rollback or restoration must return application and data to a mutually valid state without discarding sales made before the maintenance window.

### Manual outage and reconciliation

- Any serving staff member may begin the numbered-paper fallback if the local service or staff LAN has not recovered within five minutes; staff do not wait for the technical custodian. The responsible shift staff opens a **POS Service Incident**, recording observed start time, the last known live transaction, the first Outage Sale Record number, and the observed symptom.
- Each pre-numbered **Outage Sale Record** retains actual service time, service context, items and modifiers, totals, Payment method, cash tender/change where applicable, fulfillment state, responsible staff, and later corrections. Missing numbers and discrepancies remain visible.
- Both cash and Manual QR Payment remain available during an outage. A QR transfer is a Payment only when staff actually observe receipt in the bank account; a screenshot or customer assertion is insufficient. When receipt cannot be verified, the customer uses cash or the paper record remains unpaid.
- After recovery, the responsible shift staff validates both primary clients before resuming live entry. Service to customers need not wait for the whole paper backlog to be reconstructed.
- Exactly one **Outage Recovery Entry** reconstructs each paper record with its actual occurrence time and link to the paper number. Already completed preparation is not sent to the Preparation Queue again. If the original Sales Shift remains open, the entry participates in that Shift's reconciliation and must be completed before closure. If that is impossible, the Shift closes with its preserved discrepancy and the Manager records post-Shift corrections without reopening it.
- The POS Service Incident ends only after both primary clients pass the readiness check; its record retains the end time and full paper-number range.

### Daily readiness

Before accepting sales, the staff member opening the Sales Shift performs the **Daily POS Readiness Check**: the local service and LAN are available, both primary clients authenticate, system time is correct, the latest expected backup is current, numbered outage forms are ready, and Manual QR Payments can be verified. Any failed check is escalated to the POS Operations Owner before opening sales.
