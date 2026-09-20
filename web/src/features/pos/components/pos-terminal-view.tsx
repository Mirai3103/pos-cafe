import * as React from "react";
import { PlaceholderPage } from "@/components/feedback/placeholder-page";
import { ShoppingCart, DollarSign, Package, Users, Plus, CreditCard } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { formatVND } from "@/lib/utils";

const mockProducts = [
  { id: 1, name: "Cà phê Sữa Đá", category: "Cà phê", price: 29000 },
  { id: 2, name: "Bạc Xỉu Đá", category: "Cà phê", price: 32000 },
  { id: 3, name: "Trà Đào Cam Sả", category: "Trà trái cây", price: 45000 },
  { id: 4, name: "Trà Vải Hoa Hồng", category: "Trà trái cây", price: 42000 },
  { id: 5, name: "Croissant Bơ Tỏi", category: "Bánh ngọt", price: 35000 },
  { id: 6, name: "Cold Brew Cam Vàng", category: "Cà phê", price: 49000 },
];

export function PosTerminalView() {
  return (
    <PlaceholderPage
      icon={ShoppingCart}
      title="Bán hàng (Cashier Terminal)"
      description="Giao diện gọi món cảm ứng, thêm topping và thanh toán nhanh tại quầy thu ngân."
      badgeText="Chờ dữ liệu Catalog"
      stats={[
        { label: "Doanh thu hôm nay", value: formatVND(4850000), icon: DollarSign },
        { label: "Đơn đã thanh toán", value: 68, icon: Package },
        { label: "Khách đang phục vụ", value: 14, icon: Users },
        { label: "Giá trị đơn trung bình", value: formatVND(71300), icon: CreditCard },
      ]}
      actions={[
        { label: "Đơn mới (F2)", icon: Plus, variant: "default" },
        { label: "Xem giỏ hàng", icon: ShoppingCart, variant: "outline" },
      ]}
    >
      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        {/* Catalog Preview Grid */}
        <div className="md:col-span-2 flex flex-col gap-4">
          <h2 className="text-base font-semibold text-foreground">Menu mẫu tham khảo</h2>
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
            {mockProducts.map((p) => (
              <Card key={p.id} className="cursor-pointer hover:border-primary transition-colors p-4 flex flex-col justify-between min-h-24">
                <div>
                  <span className="text-2xs text-muted-foreground uppercase">{p.category}</span>
                  <h4 className="text-sm font-semibold text-foreground mt-0.5">{p.name}</h4>
                </div>
                <div className="text-sm font-bold font-mono text-primary mt-2">
                  {formatVND(p.price)}
                </div>
              </Card>
            ))}
          </div>
        </div>

        {/* Cart Simulator Box */}
        <Card className="flex flex-col justify-between">
          <CardHeader className="p-4 border-b border-border">
            <CardTitle className="text-sm font-semibold">Giỏ hàng hiện tại (#ORD-0092)</CardTitle>
          </CardHeader>
          <CardContent className="p-4 flex-1 flex flex-col justify-center items-center text-center text-muted-foreground gap-2">
            <ShoppingCart className="h-8 w-8 text-muted-foreground/50" />
            <p className="text-sm">Chưa có món nào trong đơn</p>
            <p className="text-2xs">Chọn món từ danh mục bên trái hoặc quét mã vạch</p>
          </CardContent>
          <div className="p-4 border-t border-border bg-muted/30">
            <Button className="w-full h-12 text-sm font-bold" disabled>
              Thanh toán (F9)
            </Button>
          </div>
        </Card>
      </div>
    </PlaceholderPage>
  );
}
