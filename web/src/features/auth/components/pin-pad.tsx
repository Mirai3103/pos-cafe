import { Delete } from "lucide-react";
import { cn } from "cn";
import { playTapChirp } from "@/lib/sound";

export interface PinPadProps {
  value: string;
  onChange: (next: string) => void;
  /** The server accepts 4 to 8 digits (internal/auth/domain.go). */
  maxLength?: number;
  onSubmit?: () => void;
  disabled?: boolean;
  className?: string;
}

const ROWS = [
  ["1", "2", "3"],
  ["4", "5", "6"],
  ["7", "8", "9"],
];

export function PinPad({
  value,
  onChange,
  maxLength = 8,
  disabled = false,
  className,
}: PinPadProps) {
  const append = (digit: string) => {
    if (disabled || value.length >= maxLength) return;
    playTapChirp();
    onChange(value + digit);
  };

  const backspace = () => {
    if (disabled || value.length === 0) return;
    playTapChirp();
    onChange(value.slice(0, -1));
  };

  const clear = () => {
    if (disabled || value.length === 0) return;
    playTapChirp();
    onChange("");
  };

  return (
    <div
      className={cn(
        "grid w-full max-w-[340px] sm:max-w-[360px] grid-cols-3 gap-2.5 sm:gap-3",
        className,
      )}
    >
      {ROWS.map((row) =>
        row.map((digit) => (
          <button
            key={digit}
            type="button"
            disabled={disabled}
            onClick={() => append(digit)}
            className="flex h-14 min-h-[56px] min-w-[56px] cursor-pointer select-none items-center justify-center rounded-2xl border border-border bg-card font-mono text-2xl font-bold text-foreground shadow-xs transition hover:border-primary hover:bg-primary/5 active:scale-95 focus:outline-none focus:ring-2 focus:ring-primary/20 disabled:pointer-events-none disabled:opacity-40 sm:h-16"
            aria-label={`Số ${digit}`}
          >
            {digit}
          </button>
        )),
      )}

      {/* Row 4: C (Clear all), 0, Backspace */}
      <button
        type="button"
        disabled={disabled || value.length === 0}
        onClick={clear}
        className="flex h-14 min-h-[56px] min-w-[56px] cursor-pointer select-none flex-col items-center justify-center rounded-2xl border border-rose-200 bg-rose-50/60 font-mono text-rose-700 shadow-xs transition hover:border-rose-400 hover:bg-rose-100/60 active:scale-95 focus:outline-none focus:ring-2 focus:ring-rose-500/20 disabled:pointer-events-none disabled:opacity-40 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-400 sm:h-16"
        aria-label="Xóa toàn bộ mã PIN"
      >
        <span className="text-xl font-bold leading-none">C</span>
        <span className="mt-0.5 font-sans text-[10px] font-medium text-rose-600 dark:text-rose-400">
          Xóa hết
        </span>
      </button>

      <button
        type="button"
        disabled={disabled}
        onClick={() => append("0")}
        className="flex h-14 min-h-[56px] min-w-[56px] cursor-pointer select-none items-center justify-center rounded-2xl border border-border bg-card font-mono text-2xl font-bold text-foreground shadow-xs transition hover:border-primary hover:bg-primary/5 active:scale-95 focus:outline-none focus:ring-2 focus:ring-primary/20 disabled:pointer-events-none disabled:opacity-40 sm:h-16"
        aria-label="Số 0"
      >
        0
      </button>

      <button
        type="button"
        disabled={disabled || value.length === 0}
        onClick={backspace}
        className="flex h-14 min-h-[56px] min-w-[56px] cursor-pointer select-none flex-col items-center justify-center rounded-2xl border border-border bg-muted/80 text-foreground shadow-xs transition hover:border-muted-foreground/30 hover:bg-muted active:scale-95 focus:outline-none focus:ring-2 focus:ring-muted-foreground/20 disabled:pointer-events-none disabled:opacity-40 sm:h-16"
        aria-label="Xóa 1 ký tự"
      >
        <Delete className="size-6 text-foreground" />
        <span className="mt-0.5 font-sans text-[10px] font-medium text-muted-foreground">
          Lùi 1 số
        </span>
      </button>
    </div>
  );
}
