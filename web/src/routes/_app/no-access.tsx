import { createFileRoute } from "@tanstack/react-router";
import { ShieldAlert } from "lucide-react";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export const Route = createFileRoute("/_app/no-access")({
  component: NoAccess,
});

function NoAccess() {
  return (
    <div className="flex h-full w-full items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm border-border shadow-md">
        <CardHeader className="p-6 pb-4 text-center">
          <div className="mx-auto mb-2 flex h-12 w-12 items-center justify-center rounded-xl bg-muted text-muted-foreground">
            <ShieldAlert className="size-6" />
          </div>
          <CardTitle className="text-xl font-bold">Không có quyền truy cập</CardTitle>
          <CardDescription className="text-xs text-muted-foreground">
            Tài khoản của bạn không có quyền sử dụng khu vực này. Liên hệ quản lý nếu bạn nghĩ đây là nhầm lẫn.
          </CardDescription>
        </CardHeader>
      </Card>
    </div>
  );
}
