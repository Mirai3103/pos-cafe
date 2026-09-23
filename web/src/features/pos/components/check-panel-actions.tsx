import type { ReactNode } from "react";
import { CheckCircle2, ChefHat } from "lucide-react";
import type { PosPhase } from "../utils/phase";

export interface CheckPanelActionsProps {
  phase: PosPhase;
  canCollect: boolean;
  isSubmitting: boolean;
  isClosing: boolean;
  onCollect: () => void;
  onSubmit: () => void;
  onClose: () => void;
  onNextCustomer: () => void;
}

const PRIMARY =
  "min-h-[48px] h-12 w-full rounded-xl bg-primary text-sm font-bold text-primary-foreground disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2 select-none active:scale-[0.98] transition";
const SECONDARY =
  "min-h-[48px] h-12 w-full rounded-xl border border-border bg-card text-sm font-bold text-foreground hover:bg-muted flex items-center justify-center gap-2 select-none active:scale-[0.98] transition";

function Bar({ children }: { children: ReactNode }) {
  return <div className="border-t border-border bg-muted/20 p-4 space-y-2.5 shrink-0">{children}</div>;
}

/**
 * One primary action per phase, labelled with the key that fires it.
 * F9 mapping lives in hooks/use-pos-hotkeys.ts and must match these labels.
 */
export function CheckPanelActions({
  phase,
  canCollect,
  isSubmitting,
  isClosing,
  onCollect,
  onSubmit,
  onClose,
  onNextCustomer,
}: CheckPanelActionsProps) {
  switch (phase) {
    case "AWAITING_PAYMENT":
      return (
        <Bar>
          <button type="button" onClick={onCollect} disabled={!canCollect} className={PRIMARY}>
            Thu tiền (F9)
          </button>
        </Bar>
      );
    case "AWAITING_SUBMIT":
      return (
        <Bar>
          <button type="button" onClick={onSubmit} disabled={isSubmitting} className={PRIMARY}>
            <ChefHat className="h-4 w-4" />
            {isSubmitting ? "Đang gửi bếp..." : "Gửi bếp (F9)"}
          </button>
          <button type="button" onClick={onNextCustomer} className={SECONDARY}>
            Khách tiếp theo
          </button>
        </Bar>
      );
    case "IN_PREPARATION":
      return (
        <Bar>
          <button type="button" onClick={onNextCustomer} className={SECONDARY}>
            <CheckCircle2 className="h-4 w-4" />
            Khách tiếp theo (F9)
          </button>
          <button type="button" disabled className={`${PRIMARY} flex-col gap-0`}>
            <span>Hoàn tất</span>
            <span className="text-2xs font-normal">Chờ bếp hoàn tất</span>
          </button>
        </Bar>
      );
    case "READY_TO_CLOSE":
      return (
        <Bar>
          <button type="button" onClick={onClose} disabled={isClosing} className={PRIMARY}>
            <CheckCircle2 className="h-4 w-4" />
            {isClosing ? "Đang hoàn tất..." : "Hoàn tất (F9)"}
          </button>
          <button type="button" onClick={onNextCustomer} className={SECONDARY}>
            Khách tiếp theo
          </button>
        </Bar>
      );
    default:
      return null;
  }
}
