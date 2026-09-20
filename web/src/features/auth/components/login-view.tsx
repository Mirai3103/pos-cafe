import * as React from "react";
import { Coffee, ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Link } from "@tanstack/react-router";

export function LoginView() {
  const [pin, setPin] = React.useState<string>("");

  const handleDigit = (d: string) => {
    if (pin.length < 6) setPin((prev) => prev + d);
  };

  const handleClear = () => setPin("");

  return (
    <div className="flex min-h-screen w-full items-center justify-center bg-background p-4 select-none">
      <Card className="w-full max-w-sm border-border shadow-md">
        <CardHeader className="text-center p-6 pb-4">
          <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-xl bg-primary text-primary-foreground mb-2">
            <Coffee className="h-6 w-6" />
          </div>
          <CardTitle className="text-xl font-bold">Đăng nhập Thu ngân</CardTitle>
          <CardDescription className="text-xs text-muted-foreground">
            Nhập mã PIN nhân viên để mở phiên bán hàng
          </CardDescription>
        </CardHeader>

        <CardContent className="p-6 pt-0 flex flex-col gap-4">
          {/* PIN Display */}
          <div className="flex h-12 w-full items-center justify-center rounded-lg border border-input bg-muted/40 font-mono text-2xl tracking-widest text-foreground">
            {pin ? "•".repeat(pin.length) : <span className="text-xs text-muted-foreground font-sans">Nhập PIN 4-6 số</span>}
          </div>

          {/* Touch Numpad */}
          <div className="grid grid-cols-3 gap-2">
            {["1", "2", "3", "4", "5", "6", "7", "8", "9"].map((n) => (
              <Button
                key={n}
                variant="outline"
                className="h-12 text-lg font-bold font-mono"
                onClick={() => handleDigit(n)}
              >
                {n}
              </Button>
            ))}
            <Button variant="outline" className="h-12 text-xs font-semibold text-destructive" onClick={handleClear}>
              Xóa
            </Button>
            <Button variant="outline" className="h-12 text-lg font-bold font-mono" onClick={() => handleDigit("0")}>
              0
            </Button>
            <Link to="/">
              <Button variant="default" className="h-12 w-full font-bold">
                <ArrowRight className="h-5 w-5" />
              </Button>
            </Link>
          </div>

          <div className="text-center mt-2">
            <Link to="/" className="text-xs text-primary hover:underline font-medium">
              Truy cập nhanh chế độ Demo POS →
            </Link>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
