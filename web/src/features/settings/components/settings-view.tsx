import { PlaceholderPage } from "@/components/feedback/placeholder-page";
import { Settings, Printer, RefreshCw, Sliders } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export function SettingsView() {
  return (
    <PlaceholderPage
      icon={Settings}
      title="Cài đặt hệ thống (POS Settings)"
      description="Cấu hình máy in nhiệt khổ 80mm, tỷ lệ thuế VAT và đồng bộ danh mục từ Backend."
      badgeText="Hệ thống v1.0"
      stats={[
        { label: "Máy in hoá đơn", value: "Đã kết nối", icon: Printer },
        { label: "Đồng bộ Catalog", value: "Tự động", icon: RefreshCw },
      ]}
      actions={[
        { label: "Đồng bộ ngay", icon: RefreshCw, variant: "default" },
      ]}
    >
      <Card>
        <CardHeader className="p-4">
          <CardTitle className="text-sm font-semibold">Tùy chọn thiết bị</CardTitle>
        </CardHeader>
        <CardContent className="p-4 pt-0 text-sm text-muted-foreground flex items-center gap-2">
          <Sliders className="h-4 w-4 text-primary" />
          <span>Cấu hình cổng COM máy in hoá đơn và mã két đựng tiền tự bật khi thanh toán.</span>
        </CardContent>
      </Card>
    </PlaceholderPage>
  );
}
