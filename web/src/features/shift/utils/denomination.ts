export const VND_DENOMINATIONS = [
  500_000, 200_000, 100_000, 50_000, 20_000, 10_000, 5_000, 2_000, 1_000,
] as const;

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
