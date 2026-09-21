import * as React from "react";
import { ChefHat, Coffee, ShieldCheck, ShoppingCart } from "lucide-react";
import { useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { AuthHeader } from "./auth-header";
import { useDeclareWorkspace } from "../api/use-auth";
import { rememberedWorkspace, useSessionStore, type Workspace } from "@/stores/use-session-store";
import { messageForError } from "@/lib/error-messages";
import { playSuccessChirp, playTapChirp } from "@/lib/sound";
import { cn } from "cn";

interface WorkspaceChoice {
  value: Workspace;
  label: string;
  subLabel: string;
  hint: string;
  icon: typeof ShoppingCart;
}

const CHOICES: WorkspaceChoice[] = [
  {
    value: "cashier",
    label: "Quầy thu ngân",
    subLabel: "Order & Thanh toán",
    hint: "Tự khóa sau 5 phút",
    icon: ShoppingCart,
  },
  {
    value: "preparation",
    label: "Pha chế KDS",
    subLabel: "Màn hình Barista",
    hint: "Tự khóa sau 15 phút",
    icon: ChefHat,
  },
  {
    value: "manager",
    label: "Quản lý Ca",
    subLabel: "Báo cáo & Tiền",
    hint: "Tự khóa sau 5 phút",
    icon: ShieldCheck,
  },
];

export function WorkspacePicker() {
  const navigate = useNavigate();
  const declare = useDeclareWorkspace();
  const displayName = useSessionStore((s) => s.displayName);
  const remembered = rememberedWorkspace();
  const [selected, setSelected] = React.useState<Workspace>(remembered ?? "cashier");

  const choose = (workspace: Workspace) => {
    playTapChirp();
    setSelected(workspace);
  };

  const confirm = () => {
    declare.mutate(selected, {
      onSuccess: () => {
        playSuccessChirp();
        void navigate({ to: "/" });
      },
    });
  };

  return (
    <div className="flex min-h-screen w-full flex-col bg-background font-sans select-none">
      <AuthHeader stationSubtitle="Chọn trạm làm việc" />

      <main className="flex flex-1 items-center justify-center p-4 sm:p-6 md:p-8">
        <div
          id="auth-card"
          className="relative flex w-full max-w-md flex-col items-center overflow-hidden rounded-3xl border border-border bg-card p-6 shadow-xl transition-all duration-300 sm:max-w-lg sm:p-8"
        >
          {/* Top Emerald Accent Bar */}
          <div className="absolute inset-x-0 top-0 h-1.5 bg-gradient-to-r from-emerald-500 via-teal-500 to-emerald-600" />

          {/* Header */}
          <div className="mb-6 flex flex-col items-center text-center">
            <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-2xl border border-primary/20 bg-primary/10 text-primary shadow-xs">
              <Coffee className="size-7" />
            </div>
            <h1 className="text-2xl font-bold tracking-tight text-foreground">
              Chào {displayName ?? "bạn"}!
            </h1>
            <p className="mt-1 text-xs font-normal text-muted-foreground sm:text-sm">
              Chọn trạm làm việc cho phiên này. Khu vực quyết định thời gian tự khóa màn hình.
            </p>
          </div>

          {/* Workspace Chips */}
          <div className="mb-6 flex w-full flex-col gap-3">
            <div className="flex items-center justify-between px-1">
              <span className="text-[11px] font-bold uppercase tracking-wider text-muted-foreground">
                Khu vực phân bổ:
              </span>
              <span className="font-mono text-[10px] text-muted-foreground">Chọn 1 trạm</span>
            </div>

            <div className="grid w-full grid-cols-1 gap-2.5">
              {CHOICES.map(({ value, label, subLabel, hint, icon: Icon }) => {
                const isSelected = value === selected;
                return (
                  <button
                    key={value}
                    type="button"
                    onClick={() => choose(value)}
                    className={cn(
                      "flex min-h-[56px] cursor-pointer select-none items-center justify-between gap-3 rounded-2xl border p-3 text-left transition active:scale-[0.99] focus:outline-none",
                      isSelected
                        ? "border-emerald-500 bg-emerald-50/90 text-emerald-950 ring-2 ring-emerald-500/20 shadow-xs dark:bg-emerald-950/40 dark:text-emerald-200"
                        : "border-border bg-card text-foreground shadow-2xs hover:bg-muted/70",
                    )}
                  >
                    <div className="flex items-center gap-3">
                      <div
                        className={cn(
                          "flex h-9 w-9 shrink-0 items-center justify-center rounded-xl shadow-2xs transition",
                          isSelected
                            ? "bg-emerald-600 text-white"
                            : "bg-muted text-muted-foreground",
                        )}
                      >
                        <Icon className="size-5" />
                      </div>
                      <div className="flex flex-col">
                        <span className="text-sm font-bold leading-tight">{label}</span>
                        <span className="text-xs text-muted-foreground">{subLabel}</span>
                      </div>
                    </div>
                    <span
                      className={cn(
                        "rounded-lg px-2 py-0.5 font-mono text-[11px] font-medium",
                        isSelected
                          ? "bg-emerald-200/70 text-emerald-800 dark:bg-emerald-900 dark:text-emerald-300"
                          : "bg-muted text-muted-foreground",
                      )}
                    >
                      {hint}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>

          {/* Confirm Button */}
          <Button
            type="button"
            disabled={declare.isPending}
            onClick={confirm}
            className="h-12 w-full rounded-xl bg-emerald-600 font-sans text-base font-bold text-white shadow-xs transition hover:bg-emerald-700 active:scale-[0.98]"
          >
            {declare.isPending ? "Đang lưu cấu hình..." : "Xác nhận vào trạm làm việc"}
          </Button>

          {declare.isError && (
            <p role="alert" className="mt-3 text-center text-sm font-semibold text-rose-600">
              {messageForError(declare.error)}
            </p>
          )}
        </div>
      </main>
    </div>
  );
}
