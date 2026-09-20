import * as React from "react";
import { PlaceholderPage } from "@/components/feedback/placeholder-page";
import { Receipt, Search, Filter } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatVND } from "@/lib/utils";

export function HistoryView() {
  return (
    <PlaceholderPage
      icon={Receipt}
      title="Lịch sử đơn hàng (Order History)"
      description="Tra cứu hoá đơn đã thanh toán, in lại phiếu thu ngân và kiểm tra chi tiết giao dịch."
      badgeText="Lịch sử giao dịch"
      stats={[
        { label: "Tổng số hoá đơn", value: 142, icon: Receipt },
        { label: "Tổng thu ghi nhận", value: formatVND(10250000), icon: Receipt },
      ]}
      actions={[
        { label: "Bộ lọc", icon: Filter, variant: "outline" },
        { label: "Tìm mã đơn", icon: Search, variant: "outline" },
      ]}
    >
      <Card>
        <CardHeader className="p-4">
          <CardTitle className="text-sm font-semibold">Danh sách đơn gần đây</CardTitle>
        </CardHeader>
        <CardContent className="p-4 pt-0 text-sm text-muted-foreground">
          Sẵn sàng kết nối API tra cứu lịch sử đơn hàng từ Backend Go.
        </CardContent>
      </Card>
    </PlaceholderPage>
  );
}
