import * as React from "react";
import { Lock, LogOut } from "lucide-react";
import { Button } from "@/components/ui/button";
import { PinPad } from "./pin-pad";
import { useSignOut, useUnlock } from "../api/use-auth";
import { useSessionStore } from "@/stores/use-session-store";
import { messageForError } from "@/lib/error-messages";
import { playErrorBuzz, playSuccessChirp, playTapChirp } from "@/lib/sound";
import { cn } from "cn";

export function LockOverlay() {
  const [pin, setPin] = React.useState("");
  const [isShaking, setIsShaking] = React.useState(false);
  const displayName = useSessionStore((s) => s.displayName);
  const loginCode = useSessionStore((s) => s.loginCode);
  const unlock = useUnlock();
  const signOut = useSignOut();

  const submit = React.useCallback(() => {
    if (pin.length < 4 || unlock.isPending) return;
    unlock.mutate(pin, {
      onSuccess: () => {
        playSuccessChirp();
        setPin("");
      },
      onError: () => {
        playErrorBuzz();
        setIsShaking(true);
        setTimeout(() => {
          setIsShaking(false);
          setPin("");
        }, 700);
      },
    });
  }, [pin, unlock]);

  // Physical keyboard listener matching auth.html
  React.useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (/^[0-9]$/.test(e.key)) {
        e.preventDefault();
        playTapChirp();
        setPin((prev) => (prev.length < 8 ? prev + e.key : prev));
        return;
      }

      if (e.key === "Backspace") {
        e.preventDefault();
        playTapChirp();
        setPin((prev) => prev.slice(0, -1));
        return;
      }

      if (e.key === "Escape" || e.key.toLowerCase() === "c") {
        e.preventDefault();
        playTapChirp();
        setPin("");
        return;
      }

      if (e.key === "Enter") {
        e.preventDefault();
        submit();
        return;
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [submit]);

  const slotCount = Math.max(4, pin.length);

  return (
    <div className="fixed inset-0 z-50 flex select-none items-center justify-center bg-slate-900/60 p-4 backdrop-blur-md">
      <div
        id="auth-card"
        className="relative flex w-full max-w-md flex-col items-center overflow-hidden rounded-3xl border border-border bg-card p-6 shadow-2xl transition-all duration-300 sm:max-w-lg sm:p-8"
      >
        {/* Top Emerald Accent Bar */}
        <div className="absolute inset-x-0 top-0 h-1.5 bg-gradient-to-r from-emerald-500 via-teal-500 to-emerald-600" />

        {/* Header */}
        <div className="mb-4 flex flex-col items-center text-center">
          <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-2xl border border-amber-200 bg-amber-50 text-amber-600 shadow-xs dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-400">
            <Lock className="size-7" />
          </div>
          <h2 className="text-2xl font-bold tracking-tight text-foreground">Khóa phiên trạm</h2>
          <p className="mt-1 text-xs font-normal text-muted-foreground sm:text-sm">
            {displayName ?? "Nhân viên"}
            {loginCode ? ` (${loginCode})` : ""} — Nhập mã PIN để tiếp tục ca làm việc
          </p>
        </div>

        {/* PIN Slots Display */}
        <div className="mb-4 flex w-full flex-col items-center">
          <div
            className={cn(
              "flex items-center justify-center gap-2.5 p-2 transition-transform sm:gap-3.5",
              isShaking && "animate-pin-shake",
            )}
          >
            {Array.from({ length: slotCount }).map((_, i) => {
              const isFilled = i < pin.length;
              const isError = unlock.isError;
              return (
                <div
                  key={i}
                  className={cn(
                    "flex h-12 w-12 items-center justify-center rounded-2xl border-2 transition-all duration-200 shadow-xs sm:h-14 sm:w-14",
                    isError
                      ? "border-rose-500 bg-rose-50 dark:border-rose-700 dark:bg-rose-950/40"
                      : isFilled
                        ? "border-emerald-500 bg-emerald-50/70 dark:border-emerald-700 dark:bg-emerald-950/40"
                        : "border-border bg-muted/40",
                  )}
                >
                  <div
                    className={cn(
                      "rounded-full transition-all duration-200",
                      isError
                        ? "size-4 bg-rose-600 ring-4 ring-rose-200/80 dark:bg-rose-500 dark:ring-rose-900/50"
                        : isFilled
                          ? "size-4 bg-emerald-600 ring-4 ring-emerald-200/80 animate-pop-in dark:bg-emerald-500 dark:ring-emerald-900/50"
                          : "size-3 bg-muted-foreground/40",
                    )}
                  />
                </div>
              );
            })}
          </div>

          {/* Feedback & Status Message */}
          <div className="mt-2 flex h-5 items-center justify-center text-center font-mono text-xs font-medium">
            {unlock.isPending ? (
              <span className="font-semibold text-emerald-600 dark:text-emerald-400">
                Đang xác thực mở khóa...
              </span>
            ) : unlock.isError ? (
              <span className="font-bold text-rose-600 dark:text-rose-400">
                {messageForError(unlock.error)}
              </span>
            ) : pin.length > 0 ? (
              <span className="text-muted-foreground">Đang nhập ({pin.length}/8)...</span>
            ) : (
              <span className="text-muted-foreground">Nhập mã PIN của bạn</span>
            )}
          </div>
        </div>

        {/* Large Tactile Numpad */}
        <PinPad value={pin} onChange={setPin} maxLength={8} disabled={unlock.isPending} />

        {/* Unlock Action Button */}
        <Button
          type="button"
          disabled={pin.length < 4 || unlock.isPending}
          onClick={submit}
          className="mt-4 h-12 w-full max-w-[340px] rounded-xl bg-emerald-600 font-sans text-base font-bold text-white shadow-xs transition hover:bg-emerald-700 active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-40 sm:max-w-[360px]"
        >
          {unlock.isPending ? "Đang mở khóa..." : "Mở khóa phiên trạm"}
        </Button>

        {/* Sign Out Option */}
        <div className="mt-4 w-full border-t border-border pt-3 text-center">
          <Button
            variant="ghost"
            className="h-10 text-xs font-semibold text-muted-foreground hover:text-foreground"
            disabled={signOut.isPending}
            onClick={() => {
              playTapChirp();
              signOut.mutate();
            }}
          >
            <LogOut className="mr-1.5 size-4" />
            Đăng xuất khỏi thiết bị này
          </Button>
        </div>
      </div>
    </div>
  );
}
