import { Plus, Minus } from "lucide-react";
import {
  VND_DENOMINATIONS,
  type DenominationCounts,
  formatDenomination,
} from "@/features/shift/utils/denomination";
import { formatVND } from "@/lib/utils";
import { playClick } from "@/lib/sound";

interface DenominationCalculatorProps {
  counts: DenominationCounts;
  onChange: (counts: DenominationCounts) => void;
}

export function DenominationCalculator({ counts, onChange }: DenominationCalculatorProps) {
  const handleDelta = (denom: number, delta: number) => {
    playClick();
    const current = counts[denom] || 0;
    const next = Math.max(0, current + delta);
    onChange({ ...counts, [denom]: next });
  };

  const handleInput = (denom: number, raw: string) => {
    const val = parseInt(raw, 10);
    const next = isNaN(val) ? 0 : Math.max(0, val);
    onChange({ ...counts, [denom]: next });
  };

  return (
    <div className="border border-border rounded-xl bg-card overflow-hidden">
      <div className="p-3 bg-muted/40 border-b border-border flex items-center justify-between text-xs font-semibold text-muted-foreground">
        <span>Mệnh giá</span>
        <span className="text-center">Số lượng tờ</span>
        <span className="text-right">Thành tiền</span>
      </div>
      <div className="divide-y divide-border/60 max-h-64 overflow-y-auto">
        {VND_DENOMINATIONS.map((denom) => {
          const qty = counts[denom] || 0;
          const subtotal = denom * qty;
          return (
            <div key={denom} className="px-3 py-2 flex items-center justify-between gap-2 text-sm">
              <span className="font-mono font-medium text-foreground w-28">
                {formatDenomination(denom)}
              </span>

              {/* Quantity Controls */}
              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  aria-label="Giảm số lượng tờ"
                  onClick={() => handleDelta(denom, -1)}
                  disabled={qty <= 0}
                  className="w-8 h-8 rounded-lg border border-input bg-background hover:bg-muted disabled:opacity-40 flex items-center justify-center text-foreground transition active:scale-95"
                >
                  <Minus className="w-3.5 h-3.5" />
                </button>
                <input
                  type="text"
                  inputMode="numeric"
                  value={qty === 0 ? "" : qty}
                  placeholder="0"
                  onChange={(e) => handleInput(denom, e.target.value)}
                  className="w-12 h-8 text-center font-mono font-semibold border border-input rounded-lg bg-background text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
                />
                <button
                  type="button"
                  aria-label="Tăng số lượng tờ"
                  onClick={() => handleDelta(denom, 1)}
                  className="w-8 h-8 rounded-lg border border-input bg-background hover:bg-muted flex items-center justify-center text-foreground transition active:scale-95"
                >
                  <Plus className="w-3.5 h-3.5" />
                </button>
              </div>

              {/* Subtotal */}
              <span className="font-mono text-right font-medium text-muted-foreground w-28">
                {formatVND(subtotal)}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
