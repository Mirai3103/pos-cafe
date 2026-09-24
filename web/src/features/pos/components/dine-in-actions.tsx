import type { ReactNode } from "react";
import { ArrowLeft, Banknote, CheckCircle2, ChefHat } from "lucide-react";
import { canCollect, planSendToBar, resolveDineInF9, type DineInStatus } from "../utils/dine-in";

export interface DineInActionsProps {
  status: DineInStatus;
  isShiftOpen: boolean;
  isSending: boolean;
  isClosing: boolean;
  onSend: () => void;
  onCollect: () => void;
  onClose: () => void;
  onLeave: () => void;
}

const PRIMARY =
  "min-h-[48px] h-12 w-full rounded-xl bg-primary text-sm font-bold text-primary-foreground disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2 select-none active:scale-[0.98] transition";
const SECONDARY =
  "min-h-[48px] h-12 w-full rounded-xl border border-border bg-card text-sm font-bold text-foreground hover:bg-muted disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2 select-none active:scale-[0.98] transition";

/**
 * Every action whose condition holds, the F9 one primary. The key label must
 * match resolveDineInF9, which use-pos-hotkeys.ts fires.
 */
export function DineInActions({ status, isShiftOpen, isSending, isClosing, onSend, onCollect, onClose, onLeave }: DineInActionsProps) {
  const f9 = resolveDineInF9(status, false);
  const showSend = planSendToBar(status).submit;
  const showCollect = status.openCheck !== null && !status.hasMultipleOpenChecks;
  const collectBlockedByDraft = showCollect && !canCollect(status);
  const key = (action: typeof f9, label: string): ReactNode => (f9 === action ? `${label} (F9)` : label);

  return (
    <div className="border-t border-border bg-muted/20 p-4 space-y-2.5 shrink-0">
      {showSend && (
        <button type="button" onClick={onSend} disabled={isSending || !isShiftOpen} className={f9 === "send" ? PRIMARY : SECONDARY}>
          <ChefHat className="h-4 w-4" />
          {isSending ? "Đang gửi bếp..." : key("send", "Gửi bếp")}
        </button>
      )}
      {showCollect && (
        <>
          <button type="button" onClick={onCollect} disabled={!canCollect(status)} className={f9 === "collect" ? PRIMARY : SECONDARY}>
            <Banknote className="h-4 w-4" />
            {key("collect", "Thu tiền")}
          </button>
          {collectBlockedByDraft && (
            <p className="text-2xs text-muted-foreground text-center">
              Gửi bếp hoặc xóa món đang soạn trước khi thu tiền
            </p>
          )}
        </>
      )}
      {status.canClose && (
        <button type="button" onClick={onClose} disabled={isClosing} className={f9 === "close" ? PRIMARY : SECONDARY}>
          <CheckCircle2 className="h-4 w-4" />
          {isClosing ? "Đang hoàn tất..." : key("close", "Hoàn tất")}
        </button>
      )}
      <button type="button" onClick={onLeave} className={SECONDARY}>
        <ArrowLeft className="h-4 w-4" />
        Về sơ đồ bàn
      </button>
    </div>
  );
}
