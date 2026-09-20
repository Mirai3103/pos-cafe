import { PlaceholderPage } from "@/components/feedback/placeholder-page";
import { Clock, Wallet, ArrowDownRight, ArrowUpRight, Lock } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatVND } from "@/lib/utils";

export function ShiftView() {
  return (
    <PlaceholderPage
      icon={Clock}
      title="Quản lý ca làm việc (Shift Reconciliation)"
      description="Theo dõi dòng tiền mặt đầu ca, tổng thu bán hàng và đối soát kết thúc ca thu ngân."
      badgeText="Ca đang mở"
      stats={[
        { label: "Tiền mặt đầu ca", value: formatVND(1000000), icon: Wallet },
        { label: "Thu tiền mặt", value: formatVND(3250000), icon: ArrowDownRight },
        { label: "Thu chuyển khoản", value: formatVND(1600000), icon: ArrowUpRight },
        { label: "Tổng tiền trong két", value: formatVND(4250000), icon: Wallet },
      ]}
      actions={[
        { label: "Kiểm tiền & Kết ca", icon: Lock, variant: "default" },
      ]}
    >
      <Card>
        <CardHeader className="p-4">
          <CardTitle className="text-sm font-semibold">Bảng kê tiền mặt thực tế</CardTitle>
        </CardHeader>
        <CardContent className="p-4 pt-0 text-sm text-muted-foreground">
          Sẵn sàng kết nối API bàn giao ca (`/api/v1/shift/reconcile`) của backend Go.
        </CardContent>
      </Card>
    </PlaceholderPage>
  );
}
