import { VND_DENOMINATIONS } from "@/lib/utils";

/** The slice of a Check this module reads. Orval marks every field optional. */
export interface PayableCheck {
  payments?: Array<{ change_due_vnd?: number; received_at?: string }>;
}

const MAX_TENDER_SUGGESTIONS = 4;

/**
 * Change owed back to the customer, previewed live while they type.
 *
 * The figure shown after a successful payment comes from the server's
 * change_due_vnd instead; this one only drives the keypad preview.
 */
export function changeDue(tenderedVnd: number, totalVnd: number): number {
  return Math.max(0, tenderedVnd - totalVnd);
}

/** Whether the cash on the counter covers the Check. */
export function isTenderSufficient(tenderedVnd: number, totalVnd: number): boolean {
  if (totalVnd <= 0) return false;
  return tenderedVnd >= totalVnd;
}

/**
 * Quick-tender buttons: the exact total first, then the notes above it.
 *
 * A cashier handed a 100.000 note for a 47.000 order taps one button rather
 * than typing six digits.
 */
export function suggestTenders(totalVnd: number): number[] {
  if (totalVnd <= 0) return [];

  const ascending = [...VND_DENOMINATIONS].sort((a, b) => a - b);
  const suggestions = [totalVnd];

  for (const note of ascending) {
    if (suggestions.length >= MAX_TENDER_SUGGESTIONS) break;
    if (note > totalVnd) suggestions.push(note);
  }

  return suggestions;
}

/**
 * The change the server recorded for the Check's most recent Payment.
 *
 * Payments are appended, so the newest received_at wins. A Check with no
 * payment, or a Manual QR payment carrying no cash fields, reports zero.
 */
export function latestPaymentChangeDue(check?: PayableCheck | null): number {
  const payments = check?.payments ?? [];
  if (payments.length === 0) return 0;

  const newest = payments.reduce((latest, candidate) => {
    const latestAt = latest.received_at ?? "";
    const candidateAt = candidate.received_at ?? "";
    return candidateAt > latestAt ? candidate : latest;
  });

  return newest.change_due_vnd ?? 0;
}
