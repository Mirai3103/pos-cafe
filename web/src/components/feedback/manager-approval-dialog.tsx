import { useState, useEffect, useCallback } from "react";
import { ShieldCheck, X } from "lucide-react";
import {
  useManagerApprovalStore,
  type ManagerApprovalRequest,
} from "@/stores/use-manager-approval-store";
import { useSessionStore } from "@/stores/use-session-store";
import { PinPad } from "@/features/auth/components/pin-pad";
import { Input } from "@/components/ui/input";
import { playClick, playError, playAction } from "@/lib/sound";

export function ManagerApprovalDialog() {
  const isOpen = useManagerApprovalStore((s) => s.isOpen);
  const request = useManagerApprovalStore((s) => s.request);

  if (!isOpen || !request) return null;

  return <ManagerApprovalDialogModal request={request} />;
}

function ManagerApprovalDialogModal({ request }: { request: ManagerApprovalRequest }) {
  const confirm = useManagerApprovalStore((s) => s.confirm);
  const cancel = useManagerApprovalStore((s) => s.cancel);

  const roles = useSessionStore((s) => s.roles);
  const currentLoginCode = useSessionStore((s) => s.loginCode);
  const isCurrentManager = roles.includes("MANAGER");

  const [loginCode, setLoginCode] = useState(
    isCurrentManager && currentLoginCode ? currentLoginCode : "",
  );
  const [pin, setPin] = useState("");
  const [error, setError] = useState<string | null>(null);

  const handleDigit = useCallback((digit: string) => {
    if (pin.length < 8) {
      playClick();
      setPin((prev) => prev + digit);
      setError(null);
    }
  }, [pin.length]);

  const handleBackspace = useCallback(() => {
    playClick();
    setPin((prev) => prev.slice(0, -1));
    setError(null);
  }, []);

  const handleClear = useCallback(() => {
    playClick();
    setPin("");
    setError(null);
  }, []);

  const handleConfirm = useCallback(() => {
    const trimmedCode = loginCode.trim();
    if (!trimmedCode) {
      playError();
      setError("Vui lòng nhập mã nhân viên Quản lý");
      return;
    }
    if (pin.length < 4) {
      playError();
      setError("Mã PIN Quản lý phải từ 4 đến 8 chữ số");
      return;
    }

    playAction();
    confirm({
      approverLoginCode: trimmedCode,
      managerPin: pin,
    });
  }, [loginCode, pin, confirm]);

  const handleClose = useCallback(() => {
    playClick();
    cancel();
  }, [cancel]);

  // Physical keyboard support for POS terminals
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (document.activeElement?.tagName === "INPUT") {
        if (e.key === "Escape") {
          e.preventDefault();
          handleClose();
        } else if (e.key === "Enter") {
          e.preventDefault();
          handleConfirm();
        }
        return;
      }

      if (/^[0-9]$/.test(e.key)) {
        e.preventDefault();
        handleDigit(e.key);
        return;
      }

      if (e.key === "Backspace") {
        e.preventDefault();
        handleBackspace();
        return;
      }

      if (e.key === "Escape") {
        e.preventDefault();
        handleClose();
        return;
      }

      if (e.key.toLowerCase() === "c") {
        e.preventDefault();
        handleClear();
        return;
      }

      if (e.key === "Enter") {
        e.preventDefault();
        handleConfirm();
        return;
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [handleDigit, handleBackspace, handleClear, handleConfirm, handleClose]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-sm overflow-hidden flex flex-col p-6 gap-4">
        {/* Header */}
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-xl bg-primary/10 text-primary flex items-center justify-center">
              <ShieldCheck className="w-5 h-5" />
            </div>
            <div>
              <h3 className="text-base font-bold text-foreground">{request.title}</h3>
              <p className="text-xs text-muted-foreground">{request.description}</p>
            </div>
          </div>
          <button
            type="button"
            onClick={handleClose}
            className="p-1 rounded-lg text-muted-foreground hover:bg-muted transition cursor-pointer"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Login Code Input */}
        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Mã đăng nhập Quản lý (Login Code)
          </label>
          <Input
            value={loginCode}
            onChange={(e) => setLoginCode(e.target.value.toUpperCase())}
            placeholder="VD: MGR01"
            className="font-mono uppercase tracking-wider"
            disabled={isCurrentManager}
          />
        </div>

        {/* PIN Display */}
        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Mã PIN Quản lý (4-8 số)
          </label>
          <div className="h-12 border border-input rounded-xl bg-background flex items-center justify-center font-mono text-2xl tracking-widest text-foreground select-none">
            {pin ? "•".repeat(pin.length) : <span className="text-muted-foreground text-sm font-sans">Nhập mã PIN</span>}
          </div>
        </div>

        {error && (
          <p className="text-xs text-destructive text-center font-medium">{error}</p>
        )}

        {/* Touch PinPad */}
        <PinPad
          onDigit={handleDigit}
          onBackspace={handleBackspace}
          onClear={handleClear}
          onSubmit={handleConfirm}
          disabled={pin.length < 4}
          submitLabel={request.confirmLabel ?? "Xác nhận duyệt"}
        />
      </div>
    </div>
  );
}
