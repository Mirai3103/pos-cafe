import type {
  SalesChargeAllocationResponse,
  SalesCompletedSaleResponse,
  SalesPaymentResponse,
  SalesPreparationUnitResponse,
} from "@/api/generated/models";

/**
 * Readers over the immutable Completed Sale. Every figure is the server's
 * frozen record; nothing here recomputes a price.
 */

export function completedSaleItems(sale: SalesCompletedSaleResponse): SalesChargeAllocationResponse[] {
  return (sale.checks ?? []).flatMap((check) => check.allocations ?? []);
}

export function completedSaleTotals(sale: SalesCompletedSaleResponse): {
  chargeVnd: number;
  receivedVnd: number;
} {
  return (sale.checks ?? []).reduce(
    (totals, check) => ({
      chargeVnd: totals.chargeVnd + (check.charge_vnd ?? 0),
      receivedVnd: totals.receivedVnd + (check.effective_received_vnd ?? 0),
    }),
    { chargeVnd: 0, receivedVnd: 0 },
  );
}

export function completedSalePayments(sale: SalesCompletedSaleResponse): SalesPaymentResponse[] {
  return (sale.checks ?? [])
    .flatMap((check) => check.payments ?? [])
    .filter((payment) => !payment.void);
}

const OUTCOMES: ReadonlyArray<[state: string, label: string]> = [
  ["FULFILLED", "đã giao"],
  ["CANCELLED", "hủy"],
  ["WASTED", "hỏng"],
];

export function summarizePreparation(units: SalesPreparationUnitResponse[] | undefined): string {
  const parts = OUTCOMES.flatMap(([state, label]) => {
    const count = (units ?? []).filter((unit) => unit.state === state).length;
    return count > 0 ? [`${count} món ${label}`] : [];
  });
  return parts.length > 0 ? parts.join(" · ") : "Không có món pha chế";
}

const COMPLETED_AT_FORMAT = new Intl.DateTimeFormat("vi-VN", {
  hour: "2-digit",
  minute: "2-digit",
  day: "2-digit",
  month: "2-digit",
  year: "numeric",
});

export function formatCompletedAt(iso: string | undefined): string {
  if (!iso) return "";
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? "" : COMPLETED_AT_FORMAT.format(at);
}
