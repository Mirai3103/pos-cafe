import { ChefHat, ShieldCheck, ShoppingCart } from "lucide-react";
import { useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useDeclareWorkspace } from "../api/use-auth";
import { rememberedWorkspace, useSessionStore, type Workspace } from "@/stores/use-session-store";
import { messageForError } from "@/lib/error-messages";

const CHOICES: Array<{
  value: Workspace;
  label: string;
  hint: string;
  icon: typeof ShoppingCart;
}> = [
  { value: "cashier", label: "Quầy thu ngân", hint: "Tự khóa sau 5 phút", icon: ShoppingCart },
  { value: "preparation", label: "Khu pha chế", hint: "Tự khóa sau 15 phút", icon: ChefHat },
  { value: "manager", label: "Quản lý", hint: "Tự khóa sau 5 phút", icon: ShieldCheck },
];

export function WorkspacePicker() {
  const navigate = useNavigate();
  const declare = useDeclareWorkspace();
  const displayName = useSessionStore((s) => s.displayName);
  const remembered = rememberedWorkspace();

  const choose = (workspace: Workspace) => {
    declare.mutate(workspace, {
      onSuccess: () => void navigate({ to: "/" }),
    });
  };

  return (
    <div className="flex min-h-screen w-full select-none items-center justify-center bg-background p-4">
      <Card className="w-full max-w-md border-border shadow-md">
        <CardHeader className="p-6 pb-4">
          <CardTitle className="text-xl font-bold">Chào {displayName ?? "bạn"}</CardTitle>
          <CardDescription className="text-xs text-muted-foreground">
            Chọn khu vực làm việc cho phiên này. Khu vực quyết định thời gian tự khóa màn hình.
          </CardDescription>
        </CardHeader>

        <CardContent className="flex flex-col gap-3 p-6 pt-0">
          {CHOICES.map(({ value, label, hint, icon: Icon }) => (
            <Button
              key={value}
              variant={value === remembered ? "default" : "outline"}
              disabled={declare.isPending}
              className="h-12 w-full justify-start gap-3 px-4 text-base font-semibold"
              onClick={() => choose(value)}
            >
              <Icon className="size-5" />
              <span className="flex-1 text-left">{label}</span>
              <span className="text-xs font-normal opacity-70">{hint}</span>
            </Button>
          ))}

          {declare.isError && (
            <p role="alert" className="text-center text-sm text-destructive">
              {messageForError(declare.error)}
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
