import * as React from "react";
import { X, Banknote, AlertCircle, CheckCircle2 } from "lucide-react";
import { changeDue, isTenderSufficient, suggestTenders } from "../utils/payment";
import { formatVND } from "@/lib/utils";
import { useKeypadHotkeys } from "@/hooks/use-keypad-hotkeys";
import { playTapChirp, playSuccessChirp } from "@/lib/sound";

export interface PaymentDialogProps {
  isOpen: boolean;
  serviceNumber?: string;
  /** The amount owed: the draft subtotal before commit, the Check balance after. */
  totalVnd: number;
  /** True when the draft is already committed, so confirming only takes money. */
  isCommitted: boolean;
  isSubmitting: boolean;
  errorMessage: string | null;
  /** Server-reported change. Non-null switches the dialog to its result screen. */
  changeDueVnd: number | null;
  onClose: () => void;
  onConfirm: (tenderedVnd: number) => void;
  onDone: () => void;
}

function parseTendered(raw: string): number {
  const digits = raw.replace(/\D/g, "");
  if (digits === "") return 0;
  return Number.parseInt(digits, 10);
}

export function PaymentDialog({
  isOpen,
  serviceNumber,
  totalVnd,
  isCommitted,
  isSubmitting,
  errorMessage,
  changeDueVnd,
  onClose,
  onConfirm,
  onDone,
}: PaymentDialogProps) {
  const [rawTendered, setRawTendered] = React.useState("");
  const prevIsOpenRef = React.useRef(isOpen);

  // Reset the entry each time the dialog opens, never while it is open.
  React.useEffect(() => {
    if (isOpen && !prevIsOpenRef.current) {
      // oxlint-disable-next-line react/set-state-in-effect
      setRawTendered("");
    }
    prevIsOpenRef.current = isOpen;
  }, [isOpen]);

  const isPaid = changeDueVnd !== null;
  const tendered = parseTendered(rawTendered);
  const canConfirm = isTenderSufficient(tendered, totalVnd) && !isSubmitting;

  const handleConfirm = () => {
    if (isPaid) {
      playSuccessChirp();
      onDone();
      return;
    }
    if (!canConfirm) return;
    playTapChirp();
    onConfirm(tendered);
  };

  const handleClose = () => {
    if (isSubmitting) return;
    playTapChirp();
    if (isPaid) onDone();
    else onClose();
  };

  // Digits, Backspace and C fire only when focus is outside the input, so the
  // field types naturally while the dialog still behaves like a keypad.
  useKeypadHotkeys({
    onDigit: (digit) => setRawTendered((prev) => prev + digit),
    onBackspace: () => setRawTendered((prev) => prev.slice(0, -1)),
    onClear: () => setRawTendered(""),
    onSubmit: handleConfirm,
    onClose: handleClose,
    enabled: isOpen,
  });

  if (!isOpen) return null;

  const shortfall = Math.max(0, totalVnd - tendered);
  const previewChange = changeDue(tendered, totalVnd);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-xs animate-in fade-in duration-150">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="payment-dialog-title"
        className="flex flex-col w-full max-w-md rounded-2xl border border-border bg-card shadow-2xl overflow-hidden animate-in fade-in zoom-in-95 duration-150"
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border p-4 bg-muted/20">
          <div className="flex items-center gap-3">
            <div className="h-10 w-10 rounded-xl bg-primary/10 text-primary flex items-center justify-center">
              <Banknote className="h-5 w-5" />
            </div>
            <div>
              <h3 id="payment-dialog-title" className="text-base font-bold text-foreground">
                Thanh toán
              </h3>
              <p className="text-2xs text-muted-foreground">
                {serviceNumber ? `Đơn mang đi #${serviceNumber}` : "Đơn mang đi"}
                {isCommitted ? " · Đã chốt đơn" : ""}
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={handleClose}
            aria-label="Đóng"
            className="h-12 w-12 min-h-[48px] min-w-[48px] rounded-xl text-muted-foreground hover:text-foreground hover:bg-muted flex items-center justify-center select-none active:scale-[0.98] transition"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {isPaid ? (
          /* Result screen: the cashier counts the change off this number. */
          <div className="p-6 space-y-4 text-center">
            <div className="mx-auto h-12 w-12 rounded-2xl bg-primary/10 text-primary flex items-center justify-center">
              <CheckCircle2 className="h-6 w-6" />
            </div>
            <p className="text-sm font-bold text-foreground">Đã thu tiền</p>
            <div className="space-y-1">
              <p className="text-xs uppercase tracking-wider text-muted-foreground">
                Tiền thối
              </p>
              <p className="font-mono text-4xl font-bold text-primary tabular-nums">
                {formatVND(changeDueVnd)}
              </p>
            </div>
          </div>
        ) : (
          <div className="p-5 space-y-5">
            {/* Amount owed */}
            <div className="rounded-xl border border-border bg-muted/20 p-4 text-center space-y-1">
              <p className="text-xs uppercase tracking-wider text-muted-foreground">
                Tổng cộng
              </p>
              <p className="font-mono text-3xl font-bold text-primary tabular-nums">
                {formatVND(totalVnd)}
              </p>
            </div>

            {/* Quick tender */}
            <div className="grid grid-cols-2 gap-2">
              {suggestTenders(totalVnd).map((amount, index) => (
                <button
                  key={amount}
                  type="button"
                  onClick={() => {
                    playTapChirp();
                    setRawTendered(String(amount));
                  }}
                  className="min-h-[48px] rounded-xl border border-border bg-card px-3 text-sm font-bold text-foreground hover:bg-muted select-none active:scale-[0.98] transition"
                >
                  {index === 0 ? "Đúng tiền" : formatVND(amount)}
                </button>
              ))}
            </div>

            {/* Tendered entry */}
            <div className="space-y-1.5">
              <label
                htmlFor="cash-tendered"
                className="text-xs font-bold uppercase tracking-wider text-muted-foreground"
              >
                Tiền khách đưa
              </label>
              <input
                id="cash-tendered"
                inputMode="numeric"
                autoFocus
                value={rawTendered}
                onChange={(e) => setRawTendered(e.target.value.replace(/\D/g, ""))}
                placeholder="0"
                className="w-full min-h-[48px] rounded-xl border border-border bg-card px-4 font-mono text-2xl font-bold text-foreground tabular-nums text-right outline-none focus:border-primary"
              />
            </div>

            {/* Change preview */}
            <div className="flex items-baseline justify-between rounded-xl bg-muted/30 px-4 py-3">
              {shortfall > 0 ? (
                <>
                  <span className="text-sm font-bold text-destructive">Còn thiếu</span>
                  <span className="font-mono text-xl font-bold text-destructive tabular-nums">
                    {formatVND(shortfall)}
                  </span>
                </>
              ) : (
                <>
                  <span className="text-sm font-bold text-foreground">Tiền thối</span>
                  <span className="font-mono text-xl font-bold text-primary tabular-nums">
                    {formatVND(previewChange)}
                  </span>
                </>
              )}
            </div>
          </div>
        )}

        {errorMessage && (
          <div
            role="alert"
            className="mx-5 mb-4 flex items-start gap-2 rounded-xl bg-destructive/10 text-destructive px-3 py-2.5 text-xs font-semibold"
          >
            <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
            <span>{errorMessage}</span>
          </div>
        )}

        {/* Footer */}
        <div className="border-t border-border bg-muted/20 p-4 flex gap-2">
          {!isPaid && (
            <button
              type="button"
              onClick={handleClose}
              disabled={isSubmitting}
              className="min-h-[48px] flex-1 rounded-xl border border-border bg-card text-sm font-bold text-foreground hover:bg-muted disabled:opacity-50 select-none active:scale-[0.98] transition"
            >
              Hủy (Esc)
            </button>
          )}
          <button
            type="button"
            onClick={handleConfirm}
            disabled={!isPaid && !canConfirm}
            className="min-h-[48px] flex-1 rounded-xl bg-primary text-sm font-bold text-primary-foreground disabled:opacity-50 disabled:cursor-not-allowed select-none active:scale-[0.98] transition"
          >
            {isPaid ? "Xong (Enter)" : isSubmitting ? "Đang xử lý..." : "Xác nhận (Enter)"}
          </button>
        </div>
      </div>
    </div>
  );
}
