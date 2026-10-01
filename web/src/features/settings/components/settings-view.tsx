import type { ReactElement, ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTheme } from "next-themes";
import { Info, Monitor, Moon, Palette, Sun, Volume2 } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Switch } from "@/components/ui/switch";
import { playNewOrderChime, playTapChirp } from "@/lib/sound";
import { usePreferencesStore, ZOOM_LEVELS } from "@/stores/use-preferences-store";

const THEMES = [
  { value: "light", label: "Sáng", icon: Sun },
  { value: "dark", label: "Tối", icon: Moon },
  { value: "system", label: "Theo hệ thống", icon: Monitor },
] as const;

/** Per-device preferences: every signed-in staff member may open it. */
export function SettingsView(): ReactElement {
  const { theme, setTheme } = useTheme();
  const { soundEnabled, newOrderChime, zoom, setSoundEnabled, setNewOrderChime, setZoom } = usePreferencesStore();

  return (
    <div className="h-full overflow-y-auto bg-slate-50 dark:bg-background">
      <div className="mx-auto w-full max-w-3xl space-y-6 p-4 sm:p-6 lg:p-8">
        <div>
          <h1 className="text-xl font-bold text-foreground">Cài đặt</h1>
          <p className="text-sm text-muted-foreground">Các tùy chọn chỉ áp dụng cho máy này.</p>
        </div>

        <Section icon={<Palette className="h-4 w-4" />} title="Giao diện">
          <Row label="Chế độ màu">
            <Segmented
              options={THEMES.map((t) => ({ value: t.value, label: t.label, icon: <t.icon className="h-4 w-4" /> }))}
              value={theme ?? "light"}
              onChange={setTheme}
            />
          </Row>
          <Row label="Cỡ chữ" hint="Phóng to toàn bộ màn hình">
            <Segmented
              options={ZOOM_LEVELS.map((z) => ({ value: String(z), label: `${z}%` }))}
              value={String(zoom)}
              onChange={(v) => setZoom(Number(v) as (typeof ZOOM_LEVELS)[number])}
            />
          </Row>
        </Section>

        <Section icon={<Volume2 className="h-4 w-4" />} title="Âm thanh">
          <Row label="Âm phản hồi khi bấm" hint="Tiếng bấm phím, báo thành công và báo lỗi.">
            <Switch
              checked={soundEnabled}
              onCheckedChange={(on) => {
                setSoundEnabled(on);
                if (on) playTapChirp();
              }}
              aria-label="Âm phản hồi khi bấm"
            />
          </Row>
          <Row label="Chuông báo đơn mới (Bếp KDS)" hint="Kêu khi có món mới vào hàng chờ, kể cả khi đã tắt âm phản hồi.">
            <Switch
              checked={newOrderChime}
              onCheckedChange={(on) => {
                setNewOrderChime(on);
                if (on) playNewOrderChime();
              }}
              aria-label="Chuông báo đơn mới"
            />
          </Row>
        </Section>

        <Section icon={<Info className="h-4 w-4" />} title="Thông tin">
          <Row label="Phiên bản">
            <span className="font-mono text-sm text-foreground">{import.meta.env.VITE_APP_BUILD ?? "dev"}</span>
          </Row>
          <Row label="Máy chủ">
            <span className="font-mono text-sm text-foreground">{window.location.host}</span>
          </Row>
          <Row label="Kết nối">
            <ServerStatus />
          </Row>
        </Section>
      </div>
    </div>
  );
}

function ServerStatus(): ReactElement {
  const { data, isPending } = useQuery({
    queryKey: ["health"],
    queryFn: async () => {
      const res = await fetch("/health");
      return (await res.json()) as { status?: string; database?: string };
    },
    refetchInterval: 10_000,
    retry: false,
  });

  if (isPending) return <span className="text-sm text-muted-foreground">Đang kiểm tra…</span>;
  const ok = data?.status === "healthy";
  return (
    <span className={`text-sm font-semibold ${ok ? "text-emerald-600 dark:text-emerald-400" : "text-destructive"}`}>
      {ok ? "Đã kết nối" : data?.database === "down" ? "Mất kết nối cơ sở dữ liệu" : "Mất kết nối máy chủ"}
    </span>
  );
}

function Section({ icon, title, children }: { icon: ReactNode; title: string; children: ReactNode }): ReactElement {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          {icon}
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="divide-y divide-border">{children}</CardContent>
    </Card>
  );
}

function Row({ label, hint, children }: { label: string; hint?: string; children: ReactNode }): ReactElement {
  return (
    <div className="flex flex-col gap-3 py-3 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <div className="text-sm font-semibold text-foreground">{label}</div>
        {hint && <div className="text-xs text-muted-foreground">{hint}</div>}
      </div>
      {children}
    </div>
  );
}

function Segmented({
  options,
  value,
  onChange,
}: {
  options: { value: string; label: string; icon?: ReactNode }[];
  value: string;
  onChange: (value: string) => void;
}): ReactElement {
  return (
    <div className="flex flex-wrap gap-1 rounded-xl bg-muted p-1">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={o.value === value}
          onClick={() => onChange(o.value)}
          className={`flex h-12 min-h-[48px] items-center gap-2 rounded-lg px-3 text-sm font-semibold transition ${
            o.value === value
              ? "bg-card text-foreground shadow-xs"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          {o.icon}
          {o.label}
        </button>
      ))}
    </div>
  );
}
