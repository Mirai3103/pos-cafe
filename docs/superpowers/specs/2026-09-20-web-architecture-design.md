# Specification: Web Codebase Architecture, Router, Design Tokens, and Placeholder Pages

- **Date:** 2026-09-20
- **Status:** Approved / Ready for Implementation
- **Target Directory:** `./web`
- **Design Reference:** `design-system/pos-cafe/DESIGN.md` (Crisp Emerald POS)

---

## 1. Overview & Objectives

Establish a production-grade frontend architecture for the Cafe POS web application located at `./web`. The foundation provides:
1. **Feature-based directory structure** (Vertical Slices) for high scalability and separation of domain logic.
2. **Directory-based routing** powered by `@tanstack/react-router` and `@tanstack/router-plugin/vite`.
3. **Strict Design Token System** conforming to Shadcn UI and Tailwind CSS v4 without hardcoded values (e.g. no `text-[10px]` or `bg-[#059669]`).
4. **Rich Placeholder Views** mimicking realistic POS touchscreens for Cashier, Tables, KDS, Shift, History, Settings, and Auth.
5. **Orval Codegen Integration** with the Go backend Swagger specification (`docs/swagger.yaml`) for type-safe TanStack Query v5 hooks and Zod schemas.

---

## 2. Technology Stack & Dependencies

- **Framework & Runtime:** React 19 (`react`, `react-dom`), Vite 8, TypeScript 7.
- **Styling & UI Primitives:** Tailwind CSS v4, `@base-ui/react`, Shadcn UI (`base-maia` style), `lucide-react`, `tw-animate-css`.
- **Fonts:**
  - `@fontsource-variable/outfit` (Primary sans & heading typography).
  - `@fontsource/jetbrains-mono` (Numeric metrics, monetary amounts, table codes).
- **Routing:** `@tanstack/react-router`, `@tanstack/router-plugin`.
- **State & Server Cache:** `@tanstack/react-query` (v5), `zustand`.
- **Validation & API Client:** `zod`, `axios`, `orval` (dev dependency).

---

## 3. Strict Design Tokens & Zero-Hardcoding Enforcement

### 3.1 Token Rules
- **No arbitrary CSS values:** Components must NEVER use arbitrary escape values like `text-[10px]`, `w-[350px]`, `h-[48px]`, `p-[7px]`, `bg-[#059669]`.
- **Semantic color tokens:** All colors must resolve through Tailwind semantic tokens: `bg-primary`, `text-primary-foreground`, `bg-card`, `text-card-foreground`, `bg-muted`, `text-muted-foreground`, `border-border`, `text-destructive`, `bg-accent`, `bg-secondary`.
- **Touch Targets & Spacing:** POS touch targets must use standard or designated spacing tokens:
  - `--spacing-touch: 3rem` (48px standard touch target).
  - Utility scale: `h-12` (`3rem`), `min-h-12`, `px-4`, `py-2.5`, `gap-3`, `gap-4`.
- **Radius:** `--radius: 0.75rem` (`rounded-xl` default for tactile touch feel), with `rounded-sm`, `rounded-md`, `rounded-lg`, `rounded-2xl`, `rounded-full`.
- **Typography Scale:**
  - `--text-2xs: 0.6875rem` (11px, line-height 0.875rem) for label codes and badges.
  - `text-xs` (12px), `text-sm` (14px), `text-base` (16px), `text-lg` (18px), `text-xl` (20px), `text-2xl` (24px), `text-3xl` (30px), `text-4xl` (36px).
  - Font families: `font-sans` (`Outfit Variable`), `font-mono` (`JetBrains Mono`).

### 3.2 Theme Palette (Crisp Emerald POS)
Mapped into `web/src/index.css` via `@theme inline` and CSS variables:

| Token | Light Mode Value | Dark Mode Value | Usage |
|---|---|---|---|
| `--background` | `oklch(0.985 0.005 240)` (`#f8fafc`) | `oklch(0.18 0.02 240)` (`#0f172a`) | App body background |
| `--foreground` | `oklch(0.18 0.02 240)` (`#0f172a`) | `oklch(0.985 0.005 240)` (`#f8fafc`) | Default text color |
| `--card` | `oklch(1 0 0)` (`#ffffff`) | `oklch(0.24 0.02 240)` (`#1e293b`) | Panels, Modals, Cards |
| `--card-foreground` | `oklch(0.18 0.02 240)` | `oklch(0.985 0.005 240)` | Card text |
| `--primary` | `oklch(0.58 0.16 160)` (`#059669` Emerald 600) | `oklch(0.68 0.16 160)` (`#10b981` Emerald 500) | Primary actions, Active states |
| `--primary-foreground` | `oklch(1 0 0)` (`#ffffff`) | `oklch(1 0 0)` (`#ffffff`) | Primary button text |
| `--secondary` | `oklch(0.48 0.20 270)` (`#4f46e5` Indigo) | `oklch(0.60 0.18 270)` | Secondary actions, KDS sync |
| `--secondary-foreground` | `oklch(1 0 0)` | `oklch(1 0 0)` | Secondary text |
| `--muted` | `oklch(0.95 0.01 240)` (`#f1f5f9`) | `oklch(0.28 0.02 240)` (`#334155`) | Subdued backgrounds |
| `--muted-foreground` | `oklch(0.48 0.02 240)` (`#64748b`) | `oklch(0.70 0.02 240)` (`#94a3b8`) | Secondary descriptions |
| `--accent` | `oklch(0.65 0.16 75)` (`#d97706` Amber) | `oklch(0.72 0.16 75)` (`#f59e0b`) | Warnings, Pending orders |
| `--accent-foreground` | `oklch(1 0 0)` | `oklch(0.18 0.02 240)` | Warning badge text |
| `--destructive` | `oklch(0.55 0.22 25)` (`#dc2626` Red 600) | `oklch(0.62 0.22 25)` | Void, Delete, Errors |
| `--destructive-foreground` | `oklch(1 0 0)` | `oklch(1 0 0)` | Danger button text |
| `--border` | `oklch(0.88 0.01 240)` (`#cbd5e1` Slate 300) | `oklch(0.32 0.02 240)` (`#334155`) | Borders, Dividers |
| `--input` | `oklch(0.88 0.01 240)` | `oklch(0.32 0.02 240)` | Form controls, Search |
| `--ring` | `oklch(0.58 0.16 160)` | `oklch(0.68 0.16 160)` | Focus indicators |

---

## 4. Directory Structure (Feature-based + Nested Routing)

```text
web/
├── orval.config.ts                 # Orval OpenAPI generation config
├── package.json                    # Dependencies & scripts (including "codegen")
├── vite.config.ts                  # Vite + TanStack Router + Tailwind v4 + React
├── src/
│   ├── index.css                   # Tailwind v4 theme, fonts, and Shadcn tokens
│   ├── main.tsx                    # React 19 entry point with TanStack Router
│   ├── App.tsx                     # Optional router wrapper / Error Boundary
│   │
│   ├── routes/                     # TanStack Router Directory Structure
│   │   ├── __root.tsx              # Root: QueryClientProvider, Theme, Toaster, Devtools
│   │   ├── _app.tsx                # Pathless Layout: POS Shell (Header, Nav Tabs, Clock, Shift Badge)
│   │   ├── _app/                   # POS Shell Child Routes
│   │   │   ├── index.tsx           # / -> Cashier POS Terminal
│   │   │   ├── tables.tsx          # /tables -> Table Floor Plan
│   │   │   ├── kds.tsx             # /kds -> Kitchen Display System
│   │   │   ├── shift.tsx           # /shift -> Shift Cash Reconciliation
│   │   │   ├── history.tsx         # /history -> Orders & Receipts History
│   │   │   └── settings.tsx        # /settings -> POS Settings & Catalog Sync
│   │   └── auth/                   # Independent Auth Routes (Outside POS Shell)
│   │       └── login.tsx           # /auth/login -> Cashier PIN & Staff Login
│   │
│   ├── features/                   # Domain Vertical Slices
│   │   ├── pos/                    # Cashier Terminal
│   │   │   ├── components/         # PosTerminalView, CartPreview, CatalogPreview
│   │   │   ├── hooks/              # usePosCart
│   │   │   └── types.ts
│   │   ├── tables/                 # Table & Zone Management
│   │   │   ├── components/         # TablesView, FloorPlanPreview
│   │   │   └── types.ts
│   │   ├── kds/                    # Kitchen Display System
│   │   │   ├── components/         # KdsView, TicketListPreview
│   │   │   └── types.ts
│   │   ├── shift/                  # Shift & Cash Float
│   │   │   ├── components/         # ShiftView, ShiftDrawerPreview
│   │   │   └── types.ts
│   │   ├── history/                # Order History & Receipts
│   │   │   ├── components/         # HistoryView, ReceiptViewerPreview
│   │   │   └── types.ts
│   │   ├── settings/               # System & Printer Settings
│   │   │   ├── components/         # SettingsView, ConfigSections
│   │   │   └── types.ts
│   │   └── auth/                   # Staff Authentication
│   │       ├── components/         # LoginView, PinPad
│   │       └── types.ts
│   │
│   ├── components/                 # Shared Components
│   │   ├── ui/                     # Shadcn UI primitives (button, badge, card, tabs, dialog, input, etc.)
│   │   ├── layout/                 # PosHeader, PosNavigation, PosClock, CashierBadge, QuickActions
│   │   └── feedback/               # PlaceholderPage, EmptyState, StatCard
│   │
│   ├── lib/                        # Core Utilities & Singletons
│   │   ├── api-client.ts           # Centralized Axios instance with interceptors
│   │   ├── query-client.ts         # TanStack Query Client instance
│   │   └── utils.ts                # cn, formatVND, formatDateTime helpers
│   │
│   ├── api/                        # API Codegen Directory
│   │   └── generated/              # Output from `orval` (TanStack Query hooks, types)
│   │
│   ├── stores/                     # Zustand Global Stores
│   │   ├── use-pos-store.ts        # SUPERSEDED by ADR-053, not created
│   │   └── use-shift-store.ts      # SUPERSEDED by ADR-053, not created
│   │
│   └── types/                      # Common Shared Types
│       └── index.ts
```

---

## 5. Router & Navigation Architecture

### 5.1 Route Tree Layout
- `__root.tsx`:
  - Injects `QueryClientProvider` and `Toaster`.
  - Configures global not-found and error handling boundaries.
  - Mounts `<Outlet />` with lazy-loaded TanStack Router Devtools in development.
- `_app.tsx` (Pathless Shell Layout):
  - Wraps all main POS screens.
  - Renders the tactile POS top header containing:
    - Cafe brand identity with Emerald accent.
    - Quick navigation tab pills:
      - `Bán hàng` (`/`)
      - `Sơ đồ bàn` (`/tables`)
      - `Màn hình bếp` (`/kds`)
      - `Ca làm việc` (`/shift`)
      - `Lịch sử đơn` (`/history`)
      - `Cài đặt` (`/settings`)
    - Real-time digital clock (`font-mono`).
    - Active Cashier badge & Shift indicator.
    - Lock screen button (navigates to `/auth/login`).
  - Renders `<Outlet />` inside a flexible full-height container (`h-screen overflow-hidden`).
- `auth/login.tsx`:
  - Full-screen distraction-free touchscreen PIN keypad for staff fast-switching.

---

## 6. Placeholder Pages Specification

Each placeholder page implements a concrete **Tactile POS Dashboard Mockup** using token-based Shadcn components rather than an empty "Coming Soon" stub:

1. **POS Cashier (`/`):**
   - Left pane: Category tabs, product grid with emerald price tags (`font-mono`), search input.
   - Right pane: Active cart panel, order item list with modifiers, discount input, subtotal/tax/total, tactile "Thanh toán (F9)" button.
2. **Table Management (`/tables`):**
   - KPI counters: Total Tables, Available, In-service, Reserved.
   - Zone selection tabs: Tầng 1, Tầng 2, Sân thượng.
   - Grid of interactive table cards showing status color indicators (emerald = free, amber = occupied, indigo = billing).
3. **Kitchen Display System (`/kds`):**
   - Header with active ticket counts & average prep time.
   - Kanban columns of KDS order cards: "Chờ chế biến" (amber), "Đang nấu" (emerald), "Chờ ra món".
   - Each ticket displays table number, elapsed timer (`font-mono`), and item checklist.
4. **Shift Management (`/shift`):**
   - Active shift overview card: Cashier name, Start time, Opening float amount.
   - Cash drawer breakdown table (denominations: 500k, 200k, 100k, 50k, 20k, 10k).
   - "Kết ca & Kiểm tiền" action button with confirmation dialog.
5. **Order History (`/history`):**
   - Filter bar: Date range picker, Status filter, Search by receipt ID.
   - Recent orders table with columns: Mã đơn, Bàn/Khu vực, Thời gian, Thu ngân, Tổng tiền, Trạng thái.
   - Slide-over thermal receipt preview simulator.
6. **Settings (`/settings`):**
   - Tabbed layout: "Máy in & Thiết bị", "Thuế & Phí", "Đồng bộ Catalog", "Tài khoản".
   - Hardware status indicators (Receipt printer: Connected, Barcode scanner: Ready).
   - "Đồng bộ từ Backend" trigger with last-sync timestamp.
7. **Staff Login (`/auth/login`):**
   - Cafe branding header.
   - Large numeric PIN keypad (1-9, Clear, 0, Enter) optimized for rapid touch input.
   - Active terminal badge and switch staff option.

---

## 7. Orval Integration with Go Swagger Spec

### 7.1 Configuration (`web/orval.config.ts`)
```ts
import { defineConfig } from 'orval';

export default defineConfig({
  posApi: {
    input: {
      target: '../docs/swagger.yaml',
    },
    output: {
      mode: 'tags-split',
      target: 'src/api/generated/endpoints',
      schemas: 'src/api/generated/models',
      client: 'react-query',
      httpClient: 'axios',
      override: {
        mutator: {
          path: 'src/lib/api-client.ts',
          name: 'customAxiosInstance',
        },
        query: {
          useQuery: true,
          useMutation: true,
          options: {
            staleTime: 1000 * 30, // 30s cache
          },
        },
      },
    },
  },
});
```

### 7.2 Custom Axios Client (`src/lib/api-client.ts`)
- Base URL configured from `import.meta.env.VITE_API_BASE_URL || '/api/v1'`.
- Request interceptor attaches staff auth token or session header.
- Response interceptor normalizes API errors and handles unauthorized states.

---

## 8. Verification & Acceptance Criteria

1. **Dependency Installation:**
   - Fonts `@fontsource-variable/outfit` and `@fontsource/jetbrains-mono` installed.
   - Orval installed as dev dependency.
2. **Build & Type Checking:**
   - `tsc -b` passes with zero TypeScript errors.
   - `routeTree.gen.ts` generated cleanly by `@tanstack/router-plugin`.
3. **No Arbitrary Classes:**
   - Codebase scan confirms no hardcoded arbitrary Tailwind classes (`text-[...]`, `bg-[#...]`, `w-[...]`).
4. **All Routes Reachable:**
   - Navigating between `/`, `/tables`, `/kds`, `/shift`, `/history`, `/settings`, and `/auth/login` renders smoothly with active tab indicators.
5. **Theme Consistency:**
   - Inspecting elements in browser confirms all buttons, backgrounds, badges, and text use CSS variables (`var(--primary)`, `var(--card)`, etc.).
