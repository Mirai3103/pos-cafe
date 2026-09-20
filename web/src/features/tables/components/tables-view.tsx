import * as React from "react";
import { PlaceholderPage } from "@/components/feedback/placeholder-page";
import { Grid2X2, CheckCircle2, AlertCircle, Plus } from "lucide-react";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

const mockTables = [
  { id: "T1-01", zone: "Tầng 1", status: "occupied", guests: 3, total: 115000 },
  { id: "T1-02", zone: "Tầng 1", status: "free", guests: 0, total: 0 },
  { id: "T1-03", zone: "Tầng 1", status: "free", guests: 0, total: 0 },
  { id: "T1-04", zone: "Tầng 1", status: "billing", guests: 2, total: 85000 },
  { id: "T2-01", zone: "Tầng 2", status: "occupied", guests: 4, total: 240000 },
  { id: "T2-02", zone: "Tầng 2", status: "free", guests: 0, total: 0 },
];

export function TablesView() {
  return (
    <PlaceholderPage
      icon={Grid2X2}
      title="Sơ đồ bàn (Table Management)"
      description="Quản lý vị trí bàn, chuyển bàn, gộp bàn và trạng thái phục vụ khách trực tiếp."
      badgeText="Chờ dữ liệu Bàn"
      stats={[
        { label: "Tổng số bàn", value: 24, icon: Grid2X2 },
        { label: "Bàn trống", value: 18, icon: CheckCircle2 },
        { label: "Đang phục vụ", value: 5, icon: AlertCircle },
        { label: "Chờ dọn dẹp", value: 1, icon: AlertCircle },
      ]}
      actions={[
        { label: "Thêm bàn mới", icon: Plus, variant: "default" },
      ]}
    >
      <div className="flex flex-col gap-4">
        <h2 className="text-base font-semibold text-foreground">Sơ đồ bố trí khu vực</h2>
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-6 gap-3">
          {mockTables.map((t) => (
            <Card key={t.id} className="p-4 flex flex-col justify-between min-h-28 border hover:border-primary cursor-pointer transition-colors">
              <div className="flex items-center justify-between">
                <span className="text-sm font-bold font-mono">{t.id}</span>
                <Badge
                  variant={
                    t.status === "free" ? "default" : t.status === "occupied" ? "accent" : "secondary"
                  }
                >
                  {t.status === "free" ? "Trống" : t.status === "occupied" ? "Có khách" : "Thanh toán"}
                </Badge>
              </div>
              <div className="mt-4">
                <span className="text-2xs text-muted-foreground">{t.zone}</span>
                {t.status !== "free" && (
                  <p className="text-xs font-semibold text-foreground mt-0.5">{t.guests} khách</p>
                )}
              </div>
            </Card>
          ))}
        </div>
      </div>
    </PlaceholderPage>
  );
}
