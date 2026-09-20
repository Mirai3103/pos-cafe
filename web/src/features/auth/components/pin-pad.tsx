import { Delete } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "cn";

export interface PinPadProps {
  value: string;
  onChange: (next: string) => void;
  /** The server accepts 4 to 8 digits (internal/auth/domain.go). */
  maxLength?: number;
  onSubmit?: () => void;
  disabled?: boolean;
  className?: string;
}

const DIGITS = ["1", "2", "3", "4", "5", "6", "7", "8", "9"];

export function PinPad({
  value,
  onChange,
  maxLength = 8,
  onSubmit,
  disabled = false,
  className,
}: PinPadProps) {
  const append = (digit: string) => {
    if (value.length >= maxLength) return;
    onChange(value + digit);
  };

  const backspace = () => onChange(value.slice(0, -1));

  return (
    <div className={cn("grid grid-cols-3 gap-3", className)}>
      {DIGITS.map((digit) => (
        <Button
          key={digit}
          type="button"
          variant="outline"
          disabled={disabled}
          className="h-12 font-mono text-xl font-bold"
          onClick={() => append(digit)}
        >
          {digit}
        </Button>
      ))}
      <Button
        type="button"
        variant="outline"
        disabled={disabled || value.length === 0}
        className="h-12"
        onClick={backspace}
        aria-label="Xóa một số"
      >
        <Delete className="size-5" />
      </Button>
      <Button
        type="button"
        variant="outline"
        disabled={disabled}
        className="h-12 font-mono text-xl font-bold"
        onClick={() => append("0")}
      >
        0
      </Button>
      <Button
        type="button"
        variant="default"
        disabled={disabled || value.length < 4}
        className="h-12 font-semibold"
        onClick={onSubmit}
      >
        Vào ca
      </Button>
    </div>
  );
}
