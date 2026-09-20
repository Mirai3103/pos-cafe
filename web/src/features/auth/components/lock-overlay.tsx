import * as React from "react";
import { Lock } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { PinPad } from "./pin-pad";
import { useSignOut, useUnlock } from "../api/use-auth";
import { useSessionStore } from "@/stores/use-session-store";
import { messageForError } from "@/lib/error-messages";

export function LockOverlay() {
  const [pin, setPin] = React.useState("");
  const displayName = useSessionStore((s) => s.displayName);
  const loginCode = useSessionStore((s) => s.loginCode);
  const unlock = useUnlock();
  const signOut = useSignOut();

  const submit = () => {
    if (pin.length < 4 || unlock.isPending) return;
    unlock.mutate(pin, {
      onSuccess: () => setPin(""),
      onError: () => setPin(""),
    });
  };

  return (
    <div className="fixed inset-0 z-50 flex select-none items-center justify-center bg-background/95 p-4 backdrop-blur-sm">
      <Card className="w-full max-w-sm border-border shadow-lg">
        <CardHeader className="p-6 pb-4 text-center">
          <div className="mx-auto mb-2 flex h-12 w-12 items-center justify-center rounded-xl bg-muted text-muted-foreground">
            <Lock className="size-6" />
          </div>
          <CardTitle className="text-xl font-bold">Màn hình đã khóa</CardTitle>
          <CardDescription className="text-xs text-muted-foreground">
            {displayName ?? "Nhân viên"}
            {loginCode ? ` · ${loginCode}` : ""} — nhập PIN để tiếp tục ca đang làm
          </CardDescription>
        </CardHeader>

        <CardContent className="flex flex-col gap-4 p-6 pt-0">
          <div className="flex h-12 w-full items-center justify-center rounded-xl border border-input bg-muted/40 font-mono text-2xl tracking-widest text-foreground">
            {pin ? "•".repeat(pin.length) : <span className="font-sans text-xs text-muted-foreground">Mã PIN</span>}
          </div>

          <PinPad value={pin} onChange={setPin} onSubmit={submit} disabled={unlock.isPending} />

          {unlock.isError && (
            <p role="alert" className="text-center text-sm text-destructive">
              {messageForError(unlock.error)}
            </p>
          )}

          <Button
            variant="ghost"
            className="h-12 text-sm text-muted-foreground"
            disabled={signOut.isPending}
            onClick={() => signOut.mutate()}
          >
            Đăng xuất khỏi thiết bị này
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
