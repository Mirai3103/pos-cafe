import * as React from "react";
import { Coffee } from "lucide-react";
import { useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { AuthHeader } from "./auth-header";
import { PinPad } from "./pin-pad";
import { useSignIn } from "../api/use-auth";
import { messageForError } from "@/lib/error-messages";
import { playErrorBuzz, playSuccessChirp, playTapChirp } from "@/lib/sound";
import { cn } from "@/lib/utils";
import { useKeypadHotkeys } from "@/hooks/use-keypad-hotkeys";

export function LoginView() {
  const [loginCode, setLoginCode] = React.useState("");
  const [pin, setPin] = React.useState("");
  const [isShaking, setIsShaking] = React.useState(false);
  const navigate = useNavigate();
  const signIn = useSignIn();

  const canSubmit = loginCode.trim().length > 0 && pin.length >= 4 && !signIn.isPending;

  const submit = React.useCallback(() => {
    if (!canSubmit) return;
    signIn.mutate(
      { login_code: loginCode.trim(), pin },
      {
        onSuccess: () => {
          playSuccessChirp();
          setPin("");
          void navigate({ to: "/auth/workspace" });
        },
        onError: () => {
          playErrorBuzz();
          setIsShaking(true);
          setTimeout(() => {
            setIsShaking(false);
            setPin("");
          }, 700);
        },
      },
    );
  }, [canSubmit, loginCode, pin, signIn, navigate]);

  // Physical keyboard support matching design-system/pos-cafe/pages/auth.html
  useKeypadHotkeys({
    onDigit: (digit) => {
      playTapChirp();
      setPin((prev) => (prev.length < 8 ? prev + digit : prev));
    },
    onBackspace: () => {
      playTapChirp();
      setPin((prev) => prev.slice(0, -1));
    },
    onClear: () => {
      playTapChirp();
      setPin("");
    },
    onSubmit: submit,
    enabled: !signIn.isPending,
  });


  // Determine slot count (at least 4, up to pin length)
  const slotCount = Math.max(4, pin.length);

  return (
    <div className="flex min-h-screen w-full flex-col bg-background font-sans select-none">
      {/* Top Bar matching auth.html */}
      <AuthHeader stationSubtitle="Đăng nhập PIN" />

      {/* Main Container */}
      <main className="flex flex-1 items-center justify-center p-4 sm:p-6 md:p-8">
        <div
          id="auth-card"
          className="relative flex w-full max-w-md flex-col items-center overflow-hidden rounded-3xl border border-border bg-card p-6 shadow-xl transition-all duration-300 sm:max-w-lg sm:p-8"
        >
          {/* Top Emerald Accent Bar */}
          <div className="absolute inset-x-0 top-0 h-1.5 bg-gradient-to-r from-emerald-500 via-teal-500 to-emerald-600" />

          {/* Standard Auth Header */}
          <div className="mb-4 flex flex-col items-center text-center">
            <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-2xl border border-primary/20 bg-primary/10 text-primary shadow-xs">
              <Coffee className="size-7" />
            </div>
            <h1 className="text-2xl font-bold tracking-tight text-foreground" id="auth-title">
              The Coffee Workshop
            </h1>
            <p className="mt-1 text-xs font-normal text-muted-foreground sm:text-sm" id="auth-subtitle">
              Đăng nhập ca làm việc hoặc mở khóa phiên trạm
            </p>
          </div>

          {/* Staff Login Code input */}
          <div className="mb-4 w-full max-w-[340px] sm:max-w-[360px]">
            <div className="mb-1.5 flex items-center justify-between px-1">
              <span className="text-[11px] font-bold uppercase tracking-wider text-muted-foreground">
                Mã nhân viên:
              </span>
              <span className="font-mono text-[10px] text-muted-foreground">Ví dụ: QL01, TN01</span>
            </div>
            <Input
              value={loginCode}
              onChange={(e) => setLoginCode(e.target.value.toUpperCase())}
              maxLength={24}
              placeholder="Mã nhân viên (QL01)"
              className="h-12 rounded-xl border border-border bg-muted/40 text-center font-mono text-base font-bold tracking-widest uppercase text-foreground focus:border-primary focus:bg-card focus:ring-2 focus:ring-primary/20"
              aria-label="Mã nhân viên"
            />
          </div>

          {/* PIN Display: Masked Slots with pop-in & shake */}
          <div className="mb-4 flex w-full flex-col items-center">
            <div
              className={cn(
                "flex items-center justify-center gap-2.5 p-2 transition-transform sm:gap-3.5",
                isShaking && "animate-pin-shake",
              )}
            >
              {Array.from({ length: slotCount }).map((_, i) => {
                const isFilled = i < pin.length;
                const isError = signIn.isError;
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
              {signIn.isPending ? (
                <span className="font-semibold text-emerald-600 dark:text-emerald-400">
                  Đang xác thực thông tin...
                </span>
              ) : signIn.isError ? (
                <span className="font-bold text-rose-600 dark:text-rose-400">
                  {messageForError(signIn.error)}
                </span>
              ) : pin.length > 0 ? (
                <span className="text-muted-foreground">Đang nhập ({pin.length}/8)...</span>
              ) : (
                <span className="text-muted-foreground">Nhập mã PIN 4 chữ số của bạn</span>
              )}
            </div>
          </div>

          {/* Large Tactile Numpad */}
          <PinPad value={pin} onChange={setPin} maxLength={8} disabled={signIn.isPending} />

          {/* Submit Action Button */}
          <Button
            type="button"
            disabled={!canSubmit}
            onClick={submit}
            className="mt-4 h-12 w-full max-w-[340px] rounded-xl bg-emerald-600 font-sans text-base font-bold text-white shadow-xs transition hover:bg-emerald-700 active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-40 sm:max-w-[360px]"
          >
            {signIn.isPending ? "Đang xác thực..." : "Vào ca làm việc"}
          </Button>
        </div>
      </main>
    </div>
  );
}
