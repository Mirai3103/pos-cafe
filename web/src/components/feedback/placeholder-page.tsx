import * as React from "react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import type { LucideIcon } from "lucide-react";

export interface StatItem {
  label: string;
  value: string | number;
  subtext?: string;
  icon?: LucideIcon;
}

export interface ActionItem {
  label: string;
  icon?: LucideIcon;
  variant?: "default" | "secondary" | "outline";
  onClick?: () => void;
}

export interface PlaceholderPageProps {
  icon: LucideIcon;
  title: string;
  description: string;
  badgeText?: string;
  stats?: StatItem[];
  actions?: ActionItem[];
  children?: React.ReactNode;
}

export function PlaceholderPage({
  icon: Icon,
  title,
  description,
  badgeText = "Sẵn sàng kết nối API",
  stats = [],
  actions = [],
  children,
}: PlaceholderPageProps) {
  return (
    <div className="flex flex-col gap-6 p-6 h-full overflow-y-auto">
      {/* Header Banner */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 border-b border-border pb-5">
        <div className="flex items-center gap-4">
          <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-primary/10 text-primary">
            <Icon className="h-6 w-6" />
          </div>
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-bold tracking-tight text-foreground">{title}</h1>
              <Badge variant="default">{badgeText}</Badge>
            </div>
            <p className="text-sm text-muted-foreground mt-1">{description}</p>
          </div>
        </div>

        {actions.length > 0 && (
          <div className="flex items-center gap-2">
            {actions.map((action, i) => {
              const ActionIcon = action.icon;
              return (
                <Button
                  key={i}
                  variant={action.variant || "default"}
                  className="h-10 px-4 font-medium"
                  onClick={action.onClick}
                >
                  {ActionIcon && <ActionIcon className="mr-2 h-4 w-4" />}
                  {action.label}
                </Button>
              );
            })}
          </div>
        )}
      </div>

      {/* KPI Stats Grid */}
      {stats.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {stats.map((st, i) => {
            const StatIcon = st.icon;
            return (
              <Card key={i}>
                <CardHeader className="flex flex-row items-center justify-between space-y-0 p-4 pb-2">
                  <CardTitle className="text-sm font-medium text-muted-foreground">{st.label}</CardTitle>
                  {StatIcon && <StatIcon className="h-4 w-4 text-primary" />}
                </CardHeader>
                <CardContent className="p-4 pt-0">
                  <div className="text-2xl font-bold font-mono tracking-tight text-foreground">{st.value}</div>
                  {st.subtext && <p className="text-2xs text-muted-foreground mt-1">{st.subtext}</p>}
                </CardContent>
              </Card>
            );
          })}
        </div>
      )}

      {/* Children Custom Dashboard Preview */}
      {children}
    </div>
  );
}
