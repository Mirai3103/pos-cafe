import { PlaceholderPage } from "@/components/feedback/placeholder-page";
import { ChefHat, Flame, CheckCircle, Clock } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

export function KdsView() {
  return (
    <PlaceholderPage
      icon={ChefHat}
      title="Màn hình bếp (Kitchen Display System)"
      description="Hiển thị đơn gọi món theo thời gian thực cho quầy pha chế và khu bếp chế biến."
      badgeText="WebSocket Ready"
      stats={[
        { label: "Phiếu đang chờ", value: 4, icon: Clock },
        { label: "Đang chế biến", value: 2, icon: Flame },
        { label: "Đã hoàn tất ca", value: 76, icon: CheckCircle },
        { label: "Thời gian TB", value: "3.5 phút", icon: ChefHat },
      ]}
    >
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card className="border-t-4 border-t-accent">
          <CardHeader className="p-4 pb-2 flex flex-row items-center justify-between">
            <CardTitle className="text-sm font-bold">Phiếu #TK-014 (Bàn T1-01)</CardTitle>
            <Badge variant="accent">3 phút trước</Badge>
          </CardHeader>
          <CardContent className="p-4 pt-2 text-sm flex flex-col gap-2">
            <div className="flex justify-between border-b border-border pb-1">
              <span>2x Bạc Xỉu Đá (Ít ngọt)</span>
              <span className="font-mono text-2xs text-muted-foreground">#M01</span>
            </div>
            <div className="flex justify-between">
              <span>1x Cà phê Sữa Đá</span>
              <span className="font-mono text-2xs text-muted-foreground">#M02</span>
            </div>
          </CardContent>
        </Card>
      </div>
    </PlaceholderPage>
  );
}
