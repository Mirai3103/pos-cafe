import * as React from "react";
import { Coffee } from "lucide-react";
import { useNavigate } from "@tanstack/react-router";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { PinPad } from "./pin-pad";
import { useSignIn } from "../api/use-auth";
import { messageForError } from "@/lib/error-messages";

export function LoginView() {
  const [loginCode, setLoginCode] = React.useState("");
  const [pin, setPin] = React.useState("");
  const navigate = useNavigate();
  const signIn = useSignIn();

  const canSubmit = loginCode.trim().length > 0 && pin.length >= 4 && !signIn.isPending;

  const submit = () => {
    if (!canSubmit) return;
    signIn.mutate(
      { login_code: loginCode.trim(), pin },
      {
        onSuccess: () => {
          setPin("");
          void navigate({ to: "/auth/workspace" });
        },
        onError: () => setPin(""),
      },
    );
  };

  return (
    <div className="flex min-h-screen w-full select-none items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm border-border shadow-md">
        <CardHeader className="p-6 pb-4 text-center">
          <div className="mx-auto mb-2 flex h-12 w-12 items-center justify-center rounded-xl bg-primary text-primary-foreground">
            <Coffee className="size-6" />
          </div>
          <CardTitle className="text-xl font-bold">Đăng nhập nhân viên</CardTitle>
          <CardDescription className="text-xs text-muted-foreground">
            Nhập mã nhân viên và mã PIN để mở phiên làm việc
          </CardDescription>
        </CardHeader>

        <CardContent className="flex flex-col gap-4 p-6 pt-0">
          <Input
            value={loginCode}
            onChange={(e) => setLoginCode(e.target.value.toUpperCase())}
            maxLength={24}
            autoFocus
            placeholder="Mã nhân viên, ví dụ TN01"
            className="font-mono tracking-wider"
            aria-label="Mã nhân viên"
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
          />

          <div className="flex h-12 w-full items-center justify-center rounded-xl border border-input bg-muted/40 font-mono text-2xl tracking-widest text-foreground">
            {pin ? (
              "•".repeat(pin.length)
            ) : (
              <span className="font-sans text-xs text-muted-foreground">Mã PIN 4 đến 8 số</span>
            )}
          </div>

          <PinPad value={pin} onChange={setPin} onSubmit={submit} disabled={signIn.isPending} />

          {signIn.isError && (
            <p role="alert" className="text-center text-sm text-destructive">
              {messageForError(signIn.error)}
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
