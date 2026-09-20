# Web Architecture, Router, Theme Tokens & Placeholder Pages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish the complete frontend codebase architecture in `./web`, including Crisp Emerald design tokens, Outfit & JetBrains Mono typography, TanStack Router directory-based routing, rich POS placeholder pages, and Orval Swagger codegen integration.

**Architecture:** Feature-based vertical slices (`src/features/{pos, tables, kds, shift, history, settings, auth}`) coupled with TanStack Router nested directory routes (`src/routes/_app/...` and `src/routes/auth/login.tsx`). Strict tokenization in Tailwind CSS v4 and Shadcn UI with zero arbitrary hardcoded classes.

**Tech Stack:** React 19, Vite 8, TypeScript 7, Tailwind CSS v4, Shadcn UI (`@base-ui/react`), `@fontsource-variable/outfit`, `@fontsource/jetbrains-mono`, `@tanstack/react-router`, `@tanstack/react-query` v5, Zustand, Axios, Orval, Zod.

**Spec:** `docs/superpowers/specs/2026-09-20-web-architecture-design.md`

## Global Constraints

- Working directory for frontend commands is `./web`.
- No arbitrary Tailwind escape values allowed anywhere in the codebase (no `text-[10px]`, `bg-[#059669]`, `w-[320px]`, `p-[7px]`). All styling must use theme tokens and semantic variables.
- Touch target height for POS buttons must be at least 48px (`h-12` or `min-h-12`).
- All numeric/price displays must use `font-mono` (`JetBrains Mono`).
- All routes must be type-safe and generated via `@tanstack/router-plugin/vite`.

---

### Task 1: Install Fonts and Dependencies

**Files:**
- Modify: `web/package.json`

**Interfaces:**
- Produces: Installed packages `@fontsource-variable/outfit`, `@fontsource/jetbrains-mono`, and `orval` in `web/node_modules`.

- [ ] **Step 1: Install font packages and orval**

Run from `web` folder:
```bash
cd web && bun add @fontsource-variable/outfit @fontsource/jetbrains-mono && bun add -D orval
```

- [ ] **Step 2: Verify package.json contains new packages**

Check that `web/package.json` contains `@fontsource-variable/outfit`, `@fontsource/jetbrains-mono` under dependencies, and `orval` under devDependencies.

- [ ] **Step 3: Commit**

```bash
git add web/package.json web/bun.lock
git commit -m "build(web): install outfit, jetbrains-mono fonts and orval"
```

---

### Task 2: Configure Crisp Emerald Theme Tokens and Typography in Tailwind v4

**Files:**
- Modify: `web/src/index.css`

**Interfaces:**
- Consumes: `@fontsource-variable/outfit`, `@fontsource/jetbrains-mono`, `@tailwindcss/vite`.
- Produces: CSS variable mappings for `--primary`, `--card`, `--background`, `--accent`, `--destructive`, `--radius`, `--font-sans`, `--font-mono`, and `--text-2xs`.

- [ ] **Step 1: Update `web/src/index.css` with Crisp Emerald tokens**

Replace `web/src/index.css` with the full Crisp Emerald POS theme tokens:

```css
@import "tailwindcss";
@import "tw-animate-css";
@import "shadcn/tailwind.css";
@import "@fontsource-variable/outfit";
@import "@fontsource/jetbrains-mono";

@custom-variant dark (&:is(.dark *));

@theme inline {
  --font-sans: 'Outfit Variable', -apple-system, BlinkMacSystemFont, sans-serif;
  --font-heading: 'Outfit Variable', -apple-system, BlinkMacSystemFont, sans-serif;
  --font-mono: 'JetBrains Mono', monospace;

  --text-2xs: 0.6875rem;
  --text-2xs--line-height: 0.875rem;

  --spacing-touch: 3rem;

  --color-background: var(--background);
  --color-foreground: var(--foreground);
  --color-card: var(--card);
  --color-card-foreground: var(--card-foreground);
  --color-popover: var(--popover);
  --color-popover-foreground: var(--popover-foreground);
  --color-primary: var(--primary);
  --color-primary-foreground: var(--primary-foreground);
  --color-secondary: var(--secondary);
  --color-secondary-foreground: var(--secondary-foreground);
  --color-muted: var(--muted);
  --color-muted-foreground: var(--muted-foreground);
  --color-accent: var(--accent);
  --color-accent-foreground: var(--accent-foreground);
  --color-destructive: var(--destructive);
  --color-destructive-foreground: var(--destructive-foreground);
  --color-border: var(--border);
  --color-input: var(--input);
  --color-ring: var(--ring);

  --color-sidebar: var(--sidebar);
  --color-sidebar-foreground: var(--sidebar-foreground);
  --color-sidebar-primary: var(--sidebar-primary);
  --color-sidebar-primary-foreground: var(--sidebar-primary-foreground);
  --color-sidebar-accent: var(--sidebar-accent);
  --color-sidebar-accent-foreground: var(--sidebar-accent-foreground);
  --color-sidebar-border: var(--sidebar-border);
  --color-sidebar-ring: var(--sidebar-ring);

  --radius-sm: calc(var(--radius) * 0.6);
  --radius-md: calc(var(--radius) * 0.8);
  --radius-lg: var(--radius);
  --radius-xl: calc(var(--radius) * 1.33);
  --radius-2xl: calc(var(--radius) * 1.7);
  --radius-3xl: calc(var(--radius) * 2.2);
}

:root {
  --background: oklch(0.985 0.005 240);       /* #f8fafc slate-50 */
  --foreground: oklch(0.18 0.02 240);          /* #0f172a slate-900 */
  --card: oklch(1 0 0);                        /* #ffffff white */
  --card-foreground: oklch(0.18 0.02 240);     /* #0f172a slate-900 */
  --popover: oklch(1 0 0);                     /* #ffffff */
  --popover-foreground: oklch(0.18 0.02 240);
  --primary: oklch(0.58 0.16 160);             /* #059669 emerald-600 */
  --primary-foreground: oklch(1 0 0);          /* #ffffff */
  --secondary: oklch(0.48 0.20 270);           /* #4f46e5 indigo-600 */
  --secondary-foreground: oklch(1 0 0);
  --muted: oklch(0.95 0.01 240);               /* #f1f5f9 slate-100 */
  --muted-foreground: oklch(0.48 0.02 240);    /* #64748b slate-500 */
  --accent: oklch(0.65 0.16 75);               /* #d97706 amber-600 */
  --accent-foreground: oklch(1 0 0);
  --destructive: oklch(0.55 0.22 25);          /* #dc2626 red-600 */
  --destructive-foreground: oklch(1 0 0);
  --border: oklch(0.88 0.01 240);              /* #cbd5e1 slate-300 */
  --input: oklch(0.88 0.01 240);               /* #cbd5e1 slate-300 */
  --ring: oklch(0.58 0.16 160);                /* emerald-600 focus ring */
  --radius: 0.75rem;

  --sidebar: oklch(0.985 0.005 240);
  --sidebar-foreground: oklch(0.18 0.02 240);
  --sidebar-primary: oklch(0.58 0.16 160);
  --sidebar-primary-foreground: oklch(1 0 0);
  --sidebar-accent: oklch(0.95 0.01 240);
  --sidebar-accent-foreground: oklch(0.18 0.02 240);
  --sidebar-border: oklch(0.88 0.01 240);
  --sidebar-ring: oklch(0.58 0.16 160);
}

.dark {
  --background: oklch(0.18 0.02 240);          /* #0f172a slate-900 */
  --foreground: oklch(0.985 0.005 240);        /* #f8fafc slate-50 */
  --card: oklch(0.24 0.02 240);                /* #1e293b slate-800 */
  --card-foreground: oklch(0.985 0.005 240);
  --popover: oklch(0.24 0.02 240);
  --popover-foreground: oklch(0.985 0.005 240);
  --primary: oklch(0.68 0.16 160);             /* #10b981 emerald-500 */
  --primary-foreground: oklch(1 0 0);
  --secondary: oklch(0.60 0.18 270);           /* indigo-400 */
  --secondary-foreground: oklch(1 0 0);
  --muted: oklch(0.28 0.02 240);               /* #334155 slate-700 */
  --muted-foreground: oklch(0.70 0.02 240);    /* #94a3b8 slate-400 */
  --accent: oklch(0.72 0.16 75);               /* #f59e0b amber-500 */
  --accent-foreground: oklch(0.18 0.02 240);
  --destructive: oklch(0.62 0.22 25);
  --destructive-foreground: oklch(1 0 0);
  --border: oklch(0.32 0.02 240);              /* slate-700 */
  --input: oklch(0.32 0.02 240);
  --ring: oklch(0.68 0.16 160);

  --sidebar: oklch(0.24 0.02 240);
  --sidebar-foreground: oklch(0.985 0.005 240);
  --sidebar-primary: oklch(0.68 0.16 160);
  --sidebar-primary-foreground: oklch(1 0 0);
  --sidebar-accent: oklch(0.28 0.02 240);
  --sidebar-accent-foreground: oklch(0.985 0.005 240);
  --sidebar-border: oklch(0.32 0.02 240);
  --sidebar-ring: oklch(0.68 0.16 160);
}

@layer base {
  * {
    @apply border-border outline-ring/50;
  }
  body {
    @apply bg-background text-foreground font-sans antialiased min-h-screen selection:bg-primary/20;
  }
  html {
    @apply font-sans;
  }
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/index.css
git commit -m "style(web): configure Crisp Emerald theme tokens and typography in index.css"
```

---

### Task 3: Core Lib Utilities, API Client & Orval Codegen Setup

**Files:**
- Modify: `web/src/lib/utils.ts`
- Create: `web/src/lib/query-client.ts`
- Create: `web/src/lib/api-client.ts`
- Create: `web/orval.config.ts`
- Modify: `web/package.json` (add `"codegen": "orval"`)

**Interfaces:**
- Produces:
  - `cn`, `formatVND(val: number): string`, `formatDateTime(date: Date | string): string` in `src/lib/utils.ts`
  - `queryClient` singleton in `src/lib/query-client.ts`
  - `customAxiosInstance` mutator function in `src/lib/api-client.ts`
  - `orval.config.ts` configured with `../docs/swagger.yaml`

- [ ] **Step 1: Update `web/src/lib/utils.ts`**

```ts
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatVND(amount: number): string {
  return new Intl.NumberFormat("vi-VN", {
    style: "currency",
    currency: "VND",
    maximumFractionDigits: 0,
  }).format(amount);
}

export function formatDateTime(value: Date | string | number): string {
  const d = new Date(value);
  return new Intl.DateTimeFormat("vi-VN", {
    hour: "2-digit",
    minute: "2-digit",
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  }).format(d);
}
```

- [ ] **Step 2: Create `web/src/lib/query-client.ts`**

```ts
import { QueryClient } from "@tanstack/react-query";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 60 * 2, // 2 minutes
      retry: 1,
      refetchOnWindowFocus: false,
    },
    mutations: {
      retry: 0,
    },
  },
});
```

- [ ] **Step 3: Create `web/src/lib/api-client.ts`**

```ts
import axios, { type AxiosRequestConfig, type AxiosResponse } from "axios";

export const axiosInstance = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || "/api/v1",
  timeout: 15000,
  headers: {
    "Content-Type": "application/json",
  },
});

axiosInstance.interceptors.request.use((config) => {
  const token = localStorage.getItem("pos_token");
  if (token && config.headers) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

axiosInstance.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem("pos_token");
    }
    return Promise.reject(error);
  }
);

export const customAxiosInstance = <T>(
  config: AxiosRequestConfig,
  options?: AxiosRequestConfig
): Promise<T> => {
  return axiosInstance({
    ...config,
    ...options,
  }).then((response: AxiosResponse<T>) => response.data);
};
```

- [ ] **Step 4: Create `web/orval.config.ts`**

```ts
import { defineConfig } from "orval";

export default defineConfig({
  posCafe: {
    input: {
      target: "../docs/swagger.yaml",
    },
    output: {
      mode: "tags-split",
      target: "src/api/generated/endpoints",
      schemas: "src/api/generated/models",
      client: "react-query",
      httpClient: "axios",
      override: {
        mutator: {
          path: "src/lib/api-client.ts",
          name: "customAxiosInstance",
        },
        query: {
          useQuery: true,
          useMutation: true,
          options: {
            staleTime: 1000 * 30,
          },
        },
      },
    },
  },
});
```

- [ ] **Step 5: Add `"codegen": "orval"` to `web/package.json` scripts and test run**

Update `web/package.json`:
```json
"scripts": {
  "build": "tsc -b && vite build",
  "codegen": "orval",
  "dev": "vite",
  "lint": "oxlint",
  "preview": "vite preview"
}
```

Run test:
```bash
cd web && bun run codegen
```
Verify files are created in `web/src/api/generated`.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/ web/orval.config.ts web/package.json web/src/api/generated/
git commit -m "feat(web): add core lib utils, api client, and orval codegen config"
```

---

### Task 4: UI Primitives, Feedback Components, and POS Layout Shell

**Files:**
- Create: `web/src/components/ui/badge.tsx`
- Create: `web/src/components/ui/card.tsx`
- Create: `web/src/components/feedback/placeholder-page.tsx`
- Create: `web/src/components/layout/pos-header.tsx`

**Interfaces:**
- Produces:
  - `<Badge variant="...">`
  - `<Card>`, `<CardHeader>`, `<CardTitle>`, `<CardDescription>`, `<CardContent>`
  - `<PlaceholderPage title="..." description="..." badge="..." stats={...} actions={...}>`
  - `<PosHeader />`

- [ ] **Step 1: Create `web/src/components/ui/badge.tsx`**

```tsx
import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-full px-2.5 py-0.5 text-2xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
  {
    variants: {
      variant: {
        default: "border-transparent bg-primary text-primary-foreground",
        secondary: "border-transparent bg-secondary text-secondary-foreground",
        destructive: "border-transparent bg-destructive text-destructive-foreground",
        outline: "border-border text-foreground",
        accent: "border-transparent bg-accent text-accent-foreground",
        muted: "border-transparent bg-muted text-muted-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, ...props }: BadgeProps) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props} />
  );
}
```

- [ ] **Step 2: Create `web/src/components/ui/card.tsx`**

```tsx
import * as React from "react";
import { cn } from "@/lib/utils";

export function Card({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "rounded-xl border border-border bg-card text-card-foreground shadow-xs",
        className
      )}
      {...props}
    />
  );
}

export function CardHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn("flex flex-col space-y-1.5 p-6", className)} {...props} />
  );
}

export function CardTitle({ className, ...props }: React.HTMLAttributes<HTMLHeadingElement>) {
  return (
    <h3
      className={cn("text-lg font-semibold leading-none tracking-tight", className)}
      {...props}
    />
  );
}

export function CardDescription({ className, ...props }: React.HTMLAttributes<HTMLParagraphElement>) {
  return (
    <p className={cn("text-sm text-muted-foreground", className)} {...props} />
  );
}

export function CardContent({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("p-6 pt-0", className)} {...props} />;
}

export function CardFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn("flex items-center p-6 pt-0", className)} {...props} />
  );
}
```

- [ ] **Step 3: Create `web/src/components/feedback/placeholder-page.tsx`**

```tsx
import * as React from "react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
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
```

- [ ] **Step 4: Create `web/src/components/layout/pos-header.tsx`**

```tsx
import * as React from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import {
  Coffee,
  ShoppingCart,
  Grid2X2,
  ChefHat,
  Clock,
  Receipt,
  Settings,
  Lock,
  User,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

const navItems = [
  { to: "/", label: "Bán hàng", icon: ShoppingCart },
  { to: "/tables", label: "Sơ đồ bàn", icon: Grid2X2 },
  { to: "/kds", label: "Bếp KDS", icon: ChefHat },
  { to: "/shift", label: "Ca làm việc", icon: Clock },
  { to: "/history", label: "Lịch sử", icon: Receipt },
  { to: "/settings", label: "Cài đặt", icon: Settings },
];

export function PosHeader() {
  const routerState = useRouterState();
  const currentPath = routerState.location.pathname;

  const [time, setTime] = React.useState<string>("");

  React.useEffect(() => {
    const updateTime = () => {
      const now = new Date();
      setTime(
        now.toLocaleTimeString("vi-VN", {
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        })
      );
    };
    updateTime();
    const timer = setInterval(updateTime, 1000);
    return () => clearInterval(timer);
  }, []);

  return (
    <header className="flex h-16 w-full items-center justify-between border-b border-border bg-card px-4 shrink-0 select-none">
      {/* Brand & Acronym */}
      <div className="flex items-center gap-6">
        <Link to="/" className="flex items-center gap-2.5 font-bold text-foreground">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-xs">
            <Coffee className="h-5 w-5" />
          </div>
          <div className="flex flex-col">
            <span className="text-base font-bold tracking-tight">Crisp Emerald</span>
            <span className="text-2xs font-mono text-muted-foreground uppercase">POS Terminal v1.0</span>
          </div>
        </Link>

        {/* Navigation Tabs */}
        <nav className="hidden lg:flex items-center gap-1">
          {navItems.map((item) => {
            const Icon = item.icon;
            const isActive = currentPath === item.to;
            return (
              <Link
                key={item.to}
                to={item.to}
                className={`flex items-center gap-2 rounded-lg px-3.5 py-2 text-sm font-medium transition-colors ${
                  isActive
                    ? "bg-primary text-primary-foreground shadow-xs"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                }`}
              >
                <Icon className="h-4 w-4" />
                <span>{item.label}</span>
              </Link>
            );
          })}
        </nav>
      </div>

      {/* Right Shell Controls */}
      <div className="flex items-center gap-3">
        {/* Real-time Digital Clock */}
        <div className="hidden sm:flex items-center gap-2 rounded-lg bg-muted px-3 py-1.5">
          <Clock className="h-4 w-4 text-muted-foreground" />
          <span className="text-sm font-semibold font-mono tracking-wide text-foreground">{time}</span>
        </div>

        {/* Active Cashier & Shift Badge */}
        <div className="flex items-center gap-2 border-l border-border pl-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10 text-primary">
            <User className="h-4 w-4" />
          </div>
          <div className="hidden md:flex flex-col text-left">
            <span className="text-xs font-semibold text-foreground">Thu ngân #01</span>
            <span className="text-2xs text-muted-foreground">Ca sáng (06:00 - 14:00)</span>
          </div>
          <Badge variant="secondary" className="hidden xl:inline-flex">Đang mở ca</Badge>
        </div>

        {/* Lock Screen / Logout */}
        <Link to="/auth/login">
          <Button variant="outline" size="icon" className="h-9 w-9 rounded-lg" title="Khóa màn hình">
            <Lock className="h-4 w-4 text-muted-foreground" />
          </Button>
        </Link>
      </div>
    </header>
  );
}
```

- [ ] **Step 5: Commit**

```bash
git add web/src/components/ui/ web/src/components/feedback/ web/src/components/layout/
git commit -m "feat(web): add badge, card, rich placeholder page, and pos header shell"
```

---

### Task 5: Feature Slices Implementation (Mock Dashboards & Types)

**Files:**
- Create: `web/src/features/pos/components/pos-terminal-view.tsx`
- Create: `web/src/features/tables/components/tables-view.tsx`
- Create: `web/src/features/kds/components/kds-view.tsx`
- Create: `web/src/features/shift/components/shift-view.tsx`
- Create: `web/src/features/history/components/history-view.tsx`
- Create: `web/src/features/settings/components/settings-view.tsx`
- Create: `web/src/features/auth/components/login-view.tsx`

**Interfaces:**
- Produces:
  - `<PosTerminalView />`
  - `<TablesView />`
  - `<KdsView />`
  - `<ShiftView />`
  - `<HistoryView />`
  - `<SettingsView />`
  - `<LoginView />`

- [ ] **Step 1: Create `web/src/features/pos/components/pos-terminal-view.tsx`**

```tsx
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
```

- [ ] **Step 2: Create `web/src/features/tables/components/tables-view.tsx`**

```tsx
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
```

- [ ] **Step 3: Create `web/src/features/kds/components/kds-view.tsx`**

```tsx
import * as React from "react";
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
```

- [ ] **Step 4: Create `web/src/features/shift/components/shift-view.tsx`**

```tsx
import * as React from "react";
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
```

- [ ] **Step 5: Create `web/src/features/history/components/history-view.tsx`**

```tsx
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
```

- [ ] **Step 6: Create `web/src/features/settings/components/settings-view.tsx`**

```tsx
import * as React from "react";
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
```

- [ ] **Step 7: Create `web/src/features/auth/components/login-view.tsx`**

```tsx
import * as React from "react";
import { Coffee, KeyRound, ArrowRight } from "lucide-react";
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
```

- [ ] **Step 8: Commit**

```bash
git add web/src/features/
git commit -m "feat(web): add feature views for pos, tables, kds, shift, history, settings, and login"
```

---

### Task 6: Directory-based TanStack Router Setup & Routes

**Files:**
- Create: `web/src/routes/__root.tsx`
- Create: `web/src/routes/_app.tsx`
- Create: `web/src/routes/_app/index.tsx`
- Create: `web/src/routes/_app/tables.tsx`
- Create: `web/src/routes/_app/kds.tsx`
- Create: `web/src/routes/_app/shift.tsx`
- Create: `web/src/routes/_app/history.tsx`
- Create: `web/src/routes/_app/settings.tsx`
- Create: `web/src/routes/auth/login.tsx`

**Interfaces:**
- Consumes: Features views and layout shell.
- Produces: Type-safe route tree generated into `web/src/routeTree.gen.ts`.

- [ ] **Step 1: Create `web/src/routes/__root.tsx`**

```tsx
import * as React from "react";
import { createRootRoute, Outlet } from "@tanstack/react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/lib/query-client";

export const Route = createRootRoute({
  component: RootComponent,
});

function RootComponent() {
  return (
    <QueryClientProvider client={queryClient}>
      <Outlet />
    </QueryClientProvider>
  );
}
```

- [ ] **Step 2: Create `web/src/routes/_app.tsx`**

```tsx
import * as React from "react";
import { createFileRoute, Outlet } from "@tanstack/react-router";
import { PosHeader } from "@/components/layout/pos-header";

export const Route = createFileRoute("/_app")({
  component: AppLayout,
});

function AppLayout() {
  return (
    <div className="flex h-screen w-screen flex-col overflow-hidden bg-background text-foreground">
      <PosHeader />
      <main className="flex-1 overflow-hidden">
        <Outlet />
      </main>
    </div>
  );
}
```

- [ ] **Step 3: Create `web/src/routes/_app/index.tsx`**

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { PosTerminalView } from "@/features/pos/components/pos-terminal-view";

export const Route = createFileRoute("/_app/")({
  component: PosTerminalView,
});
```

- [ ] **Step 4: Create `web/src/routes/_app/tables.tsx`**

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { TablesView } from "@/features/tables/components/tables-view";

export const Route = createFileRoute("/_app/tables")({
  component: TablesView,
});
```

- [ ] **Step 5: Create `web/src/routes/_app/kds.tsx`**

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { KdsView } from "@/features/kds/components/kds-view";

export const Route = createFileRoute("/_app/kds")({
  component: KdsView,
});
```

- [ ] **Step 6: Create `web/src/routes/_app/shift.tsx`**

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { ShiftView } from "@/features/shift/components/shift-view";

export const Route = createFileRoute("/_app/shift")({
  component: ShiftView,
});
```

- [ ] **Step 7: Create `web/src/routes/_app/history.tsx`**

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { HistoryView } from "@/features/history/components/history-view";

export const Route = createFileRoute("/_app/history")({
  component: HistoryView,
});
```

- [ ] **Step 8: Create `web/src/routes/_app/settings.tsx`**

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { SettingsView } from "@/features/settings/components/settings-view";

export const Route = createFileRoute("/_app/settings")({
  component: SettingsView,
});
```

- [ ] **Step 9: Create `web/src/routes/auth/login.tsx`**

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { LoginView } from "@/features/auth/components/login-view";

export const Route = createFileRoute("/auth/login")({
  component: LoginView,
});
```

- [ ] **Step 10: Commit**

```bash
git add web/src/routes/
git commit -m "feat(web): configure TanStack Router root and directory-based routes"
```

---

### Task 7: App Router Provider Integration & TypeScript Configuration

**Files:**
- Modify: `web/src/App.tsx`
- Modify: `web/src/main.tsx`

**Interfaces:**
- Consumes: `routeTree.gen.ts` generated by `@tanstack/router-plugin`.
- Produces: Mounted router inside DOM with type-safe `Register` interface.

- [ ] **Step 1: Update `web/src/App.tsx` to instantiate `createRouter`**

```tsx
import { createRouter, RouterProvider } from "@tanstack/react-router";
import { routeTree } from "./routeTree.gen";

export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

export default function App() {
  return <RouterProvider router={router} />;
}
```

- [ ] **Step 2: Update `web/src/main.tsx`**

```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import App from "./App";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>
);
```

- [ ] **Step 3: Trigger TanStack Router build & verify generation of `routeTree.gen.ts`**

Run from `web`:
```bash
cd web && bun run build
```
Verify `src/routeTree.gen.ts` is created and build finishes with exit code 0.

- [ ] **Step 4: Commit**

```bash
git add web/src/App.tsx web/src/main.tsx web/src/routeTree.gen.ts
git commit -m "feat(web): integrate TanStack RouterProvider in App.tsx and generate routeTree"
```

---

### Task 8: Verification & Zero-Hardcoding Audit

**Files:**
- Audit all files under `web/src/`

- [ ] **Step 1: Run linter and type-checker**

Run:
```bash
cd web && bun run lint && tsc -b
```
Expected: Zero errors.

- [ ] **Step 2: Audit for arbitrary Tailwind escape classes**

Run grep check to ensure zero arbitrary values:
```bash
grep -rnE "(text|bg|w|h|p|m)-\[[0-9]+px\]" web/src/
```
Expected: Empty output (zero matches).

- [ ] **Step 3: Test production build**

Run:
```bash
cd web && bun run build
```
Expected: Vite build succeeds with complete bundle output in `web/dist`.

- [ ] **Step 4: Final commit and verify git clean**

```bash
git status
```
Ensure everything is committed and repository is clean.
