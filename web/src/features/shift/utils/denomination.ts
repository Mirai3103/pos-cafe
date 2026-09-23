import { VND_DENOMINATIONS } from "@/lib/utils";

export { VND_DENOMINATIONS };

export type DenominationCounts = Record<number, number>;

export function calculateDenominationTotal(counts: DenominationCounts): number {
  return Object.entries(counts).reduce((sum, [denom, count]) => {
    const validCount = Math.max(0, count || 0);
    return sum + Number(denom) * validCount;
  }, 0);
}

export function formatDenomination(denom: number): string {
  return `${denom.toLocaleString("vi-VN")} đ`;
}
