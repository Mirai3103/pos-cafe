# Web Slice 1: Staff Access Session Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the mock login placeholder in `./web` with the real Staff Access Session — sign-in, workspace declaration, inactivity lock and unlock — and create the session, error, and guard primitives every later slice consumes.

**Architecture:** A Zustand store holds the session as the single client-side truth, hydrated from `GET /auth/session` at router start rather than from `localStorage`. Feature hooks in `features/auth/api/` wrap the orval-generated functions and unwrap the `{success, data, error}` envelope at that seam, so generated output stays regenerable and components never see it. Route guards live in `beforeLoad`. The server owns the lock policy; the client reports activity and reacts.

**Tech Stack:** React 19, TanStack Router + Query v5, Zustand, axios via the orval mutator, Tailwind v4 with the Crisp Emerald tokens already in `web/src/index.css`, `@base-ui/react` primitives, `bun test`.

**Spec:** [`docs/superpowers/specs/2026-09-21-web-frontend-slice-sequence-design.md`](../specs/2026-09-21-web-frontend-slice-sequence-design.md)

## Global Constraints

- **No mock data survives in the shipped path.** Every screen in this slice calls the real API.
- **No end-to-end tests and no integration tests.** Basic tests only: pure logic and store transitions, run with `bun test`. Do not add Vitest, happy-dom, or Testing Library.
- **The slice ends at a UAT gate** (Task 11). Completion is never self-certified.
- **No arbitrary Tailwind values.** Never `text-[10px]`, `bg-[#059669]`, `h-[48px]`. Use semantic tokens: `bg-primary`, `text-muted-foreground`, `border-border`, `bg-card`, `text-destructive`.
- **Touch targets are `h-12`** (`--spacing-touch: 3rem`, 48px). Radius default `rounded-xl`.
- **All user-facing copy is Vietnamese.** No internationalization layer.
- **Fonts:** `font-sans` (Outfit) for text, `font-mono` (JetBrains Mono) for PINs, codes, and numbers.
- **Session states are exactly** `"authenticated" | "locked" | "signed_out"`. The Go internal `"active"` is mapped to `"authenticated"` before it leaves the server (`internal/auth/get_session.go`); the client never sees `"active"`.
- **Workspaces are exactly** `"cashier" | "manager" | "preparation"`.
- **Roles are exactly** `"MANAGER" | "CASHIER" | "BARISTA"`.
- **PIN is 4 to 8 digits** (`internal/auth/domain.go` `pinRegex = ^\d{4,8}$`). `login_code` is 1 to 24 characters.
- **Capability strings, verbatim from `internal/auth/domain.go`:** Manager holds `catalog.view_prices`, `catalog.manage_availability`, `catalog.administer_structure`, `catalog.change_price`, `sales.operate`, `sales_shift.operate`, `preparation.operate`, `staff.administer`, `audit.inspect`, `tables.administer`. Cashier holds `catalog.view_prices`, `catalog.manage_availability`, `sales.operate`, `sales_shift.operate`. Barista holds `catalog.manage_availability`, `preparation.operate`. Note `sales_shift.operate` carries an underscore, not a dot, in its first segment.
- **Inactivity timeouts are the server's:** 5 minutes for cashier and manager, 15 minutes for preparation. The client never decides to lock.
- **`newRequestId()` and `<ManagerApprovalDialog>` are NOT built in this slice.** No auth command accepts `request_id` or `manager_pin`. They arrive in slice 2 with their first caller.

---

## File Structure

**Created:**

| File | Responsibility |
| :--- | :--- |
| `web/src/lib/unwrap.ts` | `ApiError` class and `unwrap()`. The only place the envelope shape is known. |
| `web/src/lib/unwrap.test.ts` | Tests for the above. |
| `web/src/lib/error-messages.ts` | Stable error code to Vietnamese message; `messageForError()`. |
| `web/src/lib/error-messages.test.ts` | Tests for the above. |
| `web/src/stores/use-session-store.ts` | The session as client truth: token, profile, capabilities, workspace, state. |
| `web/src/stores/use-session-store.test.ts` | Store transition tests. |
| `web/src/lib/guards.ts` | `requireAuthenticated()` and `requireCapability()` for `beforeLoad`. |
| `web/src/features/auth/api/use-auth.ts` | The seam: feature hooks over the generated auth functions. |
| `web/src/features/auth/components/pin-pad.tsx` | Touch numeric keypad, reused by login and the lock overlay. |
| `web/src/features/auth/components/workspace-picker.tsx` | Three-choice workspace declaration. |
| `web/src/features/auth/components/lock-overlay.tsx` | Full-screen unlock surface. |
| `web/src/components/ui/input.tsx` | Text input primitive; `components/ui/` has none yet. |
| `web/src/hooks/use-activity-ping.ts` | Throttled `POST /auth/activity` on real interaction. |
| `scripts/dev-seed.ts` | Development-only bootstrap of a Manager and a small catalog. |

**Modified:**

| File | Change |
| :--- | :--- |
| `web/src/lib/api-client.ts` | Read the token from the store; normalize every failure into `ApiError`. |
| `web/src/lib/query-client.ts` | Global `403` handling that disambiguates lock from authority denial. |
| `web/src/features/auth/components/login-view.tsx` | Rewritten against the real sign-in endpoint. |
| `web/src/routes/__root.tsx` | Hydrate the session before the first render. |
| `web/src/routes/_app.tsx` | Guard, lock overlay mount, activity ping. |
| `web/src/routes/_app/index.tsx`, `kds.tsx`, `shift.tsx`, `settings.tsx` | Per-route capability guards. |
| `web/src/components/layout/pos-header.tsx` | Staff identity, manual lock, sign out. |
| `web/package.json` | Add `"test": "bun test"`. |
| `Makefile` | Add the `dev-seed` target. |

---

### Task 1: Envelope unwrapping, ApiError, and the test runner

**Files:**
- Create: `web/src/lib/unwrap.ts`
- Create: `web/src/lib/unwrap.test.ts`
- Create: `web/src/lib/error-messages.ts`
- Create: `web/src/lib/error-messages.test.ts`
- Modify: `web/package.json`

**Interfaces:**
- Consumes: nothing.
- Produces: `class ApiError extends Error` with readonly `status: number` and `code: string`; `unwrap<T>(res: ApiEnvelope<T>): T`; `interface ApiEnvelope<T>`; `messageForError(error: unknown): string`; `ERROR_MESSAGES: Record<string, string>`.

Background the implementer needs: every Go response is `{success, data?, error?: {code, message}}` (`internal/response/response.go`). Orval therefore types every hook's payload as optional, for example `PostAuthSignIn200 = ResponseAPIResponse & { data?: AuthSignInResponse }`. The stable codes the server emits are `NOT_FOUND`, `CONFLICT`, `FINAL_ENABLED_MANAGER_REQUIRED`, `BAD_REQUEST`, `UNAUTHORIZED`, `FORBIDDEN`, `TOO_MANY_REQUESTS`, and `INTERNAL_ERROR`.

- [ ] **Step 1: Add the test script**

In `web/package.json`, inside `"scripts"`, after the `"lint"` line:

```json
    "test": "bun test",
```

- [ ] **Step 2: Write the failing tests for unwrap**

Create `web/src/lib/unwrap.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { ApiError, unwrap } from "./unwrap";

describe("unwrap", () => {
  it("returns the payload of a successful envelope", () => {
    expect(unwrap({ success: true, data: { id: "s1" } })).toEqual({ id: "s1" });
  });

  it("throws ApiError carrying the server code and message", () => {
    expect(() =>
      unwrap({ success: false, error: { code: "UNAUTHORIZED", message: "sai mã PIN" } }),
    ).toThrow(ApiError);
  });

  it("preserves the server code on the thrown error", () => {
    try {
      unwrap({ success: false, error: { code: "TOO_MANY_REQUESTS", message: "thử lại sau" } });
      throw new Error("unwrap should have thrown");
    } catch (error) {
      expect(error).toBeInstanceOf(ApiError);
      expect((error as ApiError).code).toBe("TOO_MANY_REQUESTS");
      expect((error as ApiError).message).toBe("thử lại sau");
    }
  });

  it("throws when success is true but data is absent", () => {
    expect(() => unwrap({ success: true })).toThrow(ApiError);
  });

  it("does not treat a false-y payload as absent", () => {
    expect(unwrap<number>({ success: true, data: 0 })).toBe(0);
  });
});
```

- [ ] **Step 3: Run the tests and verify they fail**

Run: `cd web && bun test src/lib/unwrap.test.ts`
Expected: FAIL, cannot resolve `./unwrap`.

- [ ] **Step 4: Implement unwrap**

Create `web/src/lib/unwrap.ts`:

```ts
export interface ApiEnvelope<T> {
  success?: boolean;
  data?: T;
  error?: { code?: string; message?: string };
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

/**
 * Narrows a Go API envelope to its payload.
 *
 * Kept out of the axios mutator on purpose: unwrapping there would make the
 * orval-generated types describe a runtime shape that no longer exists.
 */
export function unwrap<T>(res: ApiEnvelope<T>): T {
  if (res.success === false || res.error) {
    throw new ApiError(0, res.error?.code ?? "UNKNOWN", res.error?.message ?? "Đã xảy ra lỗi");
  }
  if (res.data === undefined || res.data === null) {
    throw new ApiError(0, "EMPTY_RESPONSE", "Máy chủ trả về dữ liệu rỗng");
  }
  return res.data;
}
```

- [ ] **Step 5: Run the tests and verify they pass**

Run: `cd web && bun test src/lib/unwrap.test.ts`
Expected: PASS, 5 tests.

- [ ] **Step 6: Write the failing tests for error messages**

Create `web/src/lib/error-messages.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { ApiError } from "./unwrap";
import { messageForError } from "./error-messages";

describe("messageForError", () => {
  it("maps a known code to its Vietnamese message", () => {
    expect(messageForError(new ApiError(401, "UNAUTHORIZED", "unauthorized"))).toBe(
      "Phiên đăng nhập không hợp lệ hoặc đã hết hạn.",
    );
  });

  it("falls back to the server message for an unmapped code", () => {
    expect(messageForError(new ApiError(409, "SHIFT_ALREADY_OPEN", "ca làm việc đang mở"))).toBe(
      "ca làm việc đang mở",
    );
  });

  it("returns a generic message for a non-ApiError", () => {
    expect(messageForError(new Error("socket hang up"))).toBe(
      "Không kết nối được máy chủ. Kiểm tra mạng nội bộ rồi thử lại.",
    );
  });
});
```

- [ ] **Step 7: Run the tests and verify they fail**

Run: `cd web && bun test src/lib/error-messages.test.ts`
Expected: FAIL, cannot resolve `./error-messages`.

- [ ] **Step 8: Implement the error message map**

Create `web/src/lib/error-messages.ts`:

```ts
import { ApiError } from "./unwrap";

/** Stable codes emitted by internal/response/response.go. */
export const ERROR_MESSAGES: Record<string, string> = {
  UNAUTHORIZED: "Phiên đăng nhập không hợp lệ hoặc đã hết hạn.",
  FORBIDDEN: "Bạn không có quyền thực hiện thao tác này.",
  BAD_REQUEST: "Dữ liệu gửi lên không hợp lệ.",
  NOT_FOUND: "Không tìm thấy dữ liệu.",
  CONFLICT: "Thao tác xung đột với trạng thái hiện tại.",
  TOO_MANY_REQUESTS: "Bạn thử quá nhiều lần. Chờ một lát rồi thử lại.",
  INTERNAL_ERROR: "Máy chủ gặp sự cố. Báo quản lý nếu tình trạng tiếp diễn.",
  EMPTY_RESPONSE: "Máy chủ trả về dữ liệu rỗng.",
};

const NETWORK_MESSAGE = "Không kết nối được máy chủ. Kiểm tra mạng nội bộ rồi thử lại.";

export function messageForError(error: unknown): string {
  if (error instanceof ApiError) {
    return ERROR_MESSAGES[error.code] ?? error.message;
  }
  return NETWORK_MESSAGE;
}
```

- [ ] **Step 9: Run the full suite and verify it passes**

Run: `cd web && bun test`
Expected: PASS, 8 tests across 2 files.

- [ ] **Step 10: Commit**

```bash
git add web/src/lib/unwrap.ts web/src/lib/unwrap.test.ts web/src/lib/error-messages.ts web/src/lib/error-messages.test.ts web/package.json
git commit -m "feat(web): unwrap API envelopes into typed ApiError"
```

---

### Task 2: The session store

**Files:**
- Create: `web/src/stores/use-session-store.ts`
- Create: `web/src/stores/use-session-store.test.ts`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `useSessionStore` (Zustand); types `SessionState`, `Workspace`, `SessionSnapshot`; constants `TOKEN_STORAGE_KEY = "pos_token"`, `WORKSPACE_STORAGE_KEY = "pos_workspace"`; actions `signIn(token, profile)`, `applyServerState(state)`, `lock()`, `clear()`; selector `hasCapability(capability)`.

The token continues to live under the `localStorage` key `pos_token`, which `api-client.ts` already reads. `localStorage` is a cache for the token only — never the source of truth for whether the session is valid. That answer comes from `GET /auth/session` in Task 8.

- [ ] **Step 1: Write the failing tests**

Create `web/src/stores/use-session-store.test.ts`:

```ts
import { beforeEach, describe, expect, it } from "bun:test";
import { useSessionStore } from "./use-session-store";

const profile = {
  staffId: "staff-1",
  displayName: "Nguyen Thu Ngan",
  loginCode: "TN01",
  roles: ["CASHIER"],
  capabilities: ["sales.operate", "sales_shift.operate"],
};

describe("useSessionStore", () => {
  beforeEach(() => {
    useSessionStore.getState().clear();
  });

  it("starts signed out", () => {
    expect(useSessionStore.getState().state).toBe("signed_out");
    expect(useSessionStore.getState().token).toBeNull();
  });

  it("moves to authenticated on sign in", () => {
    useSessionStore.getState().signIn("tok-1", profile);
    const s = useSessionStore.getState();
    expect(s.state).toBe("authenticated");
    expect(s.token).toBe("tok-1");
    expect(s.displayName).toBe("Nguyen Thu Ngan");
  });

  it("locks without discarding the token, because lock suspends authority rather than ending the session", () => {
    useSessionStore.getState().signIn("tok-1", profile);
    useSessionStore.getState().lock();
    const s = useSessionStore.getState();
    expect(s.state).toBe("locked");
    expect(s.token).toBe("tok-1");
    expect(s.staffId).toBe("staff-1");
  });

  it("returns to authenticated when the server reports the session unlocked", () => {
    useSessionStore.getState().signIn("tok-1", profile);
    useSessionStore.getState().lock();
    useSessionStore.getState().applyServerState({
      state: "authenticated",
      staff_id: "staff-1",
      display_name: "Nguyen Thu Ngan",
      login_code: "TN01",
      roles: ["CASHIER"],
      capabilities: ["sales.operate"],
      workspace: "cashier",
    });
    const s = useSessionStore.getState();
    expect(s.state).toBe("authenticated");
    expect(s.workspace).toBe("cashier");
  });

  it("clears everything when the server reports signed_out", () => {
    useSessionStore.getState().signIn("tok-1", profile);
    useSessionStore.getState().applyServerState({ state: "signed_out" });
    const s = useSessionStore.getState();
    expect(s.state).toBe("signed_out");
    expect(s.token).toBeNull();
    expect(s.capabilities).toEqual([]);
  });

  it("reports capabilities it holds and rejects ones it does not", () => {
    useSessionStore.getState().signIn("tok-1", profile);
    expect(useSessionStore.getState().hasCapability("sales.operate")).toBe(true);
    expect(useSessionStore.getState().hasCapability("staff.administer")).toBe(false);
  });

  it("holds no capability while locked", () => {
    useSessionStore.getState().signIn("tok-1", profile);
    useSessionStore.getState().lock();
    expect(useSessionStore.getState().hasCapability("sales.operate")).toBe(false);
  });
});
```

- [ ] **Step 2: Run the tests and verify they fail**

Run: `cd web && bun test src/stores/use-session-store.test.ts`
Expected: FAIL, cannot resolve `./use-session-store`.

- [ ] **Step 3: Implement the store**

Create `web/src/stores/use-session-store.ts`:

```ts
import { create } from "zustand";
import type { AuthSessionStateResponse } from "@/api/generated/models";

export type SessionState = "authenticated" | "locked" | "signed_out";
export type Workspace = "cashier" | "manager" | "preparation";

export const TOKEN_STORAGE_KEY = "pos_token";
export const WORKSPACE_STORAGE_KEY = "pos_workspace";

export interface StaffProfile {
  staffId: string;
  displayName: string;
  loginCode: string;
  roles: string[];
  capabilities: string[];
}

export interface SessionSnapshot {
  state: SessionState;
  token: string | null;
  staffId: string | null;
  displayName: string | null;
  loginCode: string | null;
  roles: string[];
  capabilities: string[];
  workspace: Workspace | null;
}

interface SessionActions {
  signIn: (token: string, profile: StaffProfile) => void;
  applyServerState: (state: AuthSessionStateResponse) => void;
  setWorkspace: (workspace: Workspace) => void;
  lock: () => void;
  clear: () => void;
  hasCapability: (capability: string) => boolean;
}

const EMPTY: SessionSnapshot = {
  state: "signed_out",
  token: null,
  staffId: null,
  displayName: null,
  loginCode: null,
  roles: [],
  capabilities: [],
  workspace: null,
};

function readToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_STORAGE_KEY);
  } catch {
    return null;
  }
}

function writeToken(token: string | null): void {
  try {
    if (token === null) localStorage.removeItem(TOKEN_STORAGE_KEY);
    else localStorage.setItem(TOKEN_STORAGE_KEY, token);
  } catch {
    // A locked-down browser profile must not break sign-in.
  }
}

export const useSessionStore = create<SessionSnapshot & SessionActions>()((set, get) => ({
  ...EMPTY,
  token: readToken(),

  signIn: (token, profile) => {
    writeToken(token);
    set({
      state: "authenticated",
      token,
      staffId: profile.staffId,
      displayName: profile.displayName,
      loginCode: profile.loginCode,
      roles: profile.roles,
      capabilities: profile.capabilities,
    });
  },

  applyServerState: (serverState) => {
    // The generated model types `state` as an open string, so anything that is
    // not one of the two live states is treated as signed out rather than
    // optimistically assumed to be a working session.
    if (serverState.state !== "authenticated" && serverState.state !== "locked") {
      writeToken(null);
      set({ ...EMPTY });
      return;
    }
    set({
      state: serverState.state === "locked" ? "locked" : "authenticated",
      staffId: serverState.staff_id ?? null,
      displayName: serverState.display_name ?? null,
      loginCode: serverState.login_code ?? null,
      roles: serverState.roles ?? [],
      capabilities: serverState.capabilities ?? [],
      workspace: (serverState.workspace as Workspace | undefined) ?? get().workspace,
    });
  },

  setWorkspace: (workspace) => {
    try {
      localStorage.setItem(WORKSPACE_STORAGE_KEY, workspace);
    } catch {
      // Remembering the choice is a convenience, not a requirement.
    }
    set({ workspace });
  },

  // Lock suspends authority; CONTEXT.md is explicit that it does not end the
  // Staff Access Session, so the token and identity survive it.
  lock: () => set({ state: "locked" }),

  clear: () => {
    writeToken(null);
    set({ ...EMPTY });
  },

  hasCapability: (capability) => {
    const s = get();
    if (s.state !== "authenticated") return false;
    return s.capabilities.includes(capability);
  },
}));

export function rememberedWorkspace(): Workspace | null {
  try {
    const value = localStorage.getItem(WORKSPACE_STORAGE_KEY);
    return value === "cashier" || value === "manager" || value === "preparation" ? value : null;
  } catch {
    return null;
  }
}
```

- [ ] **Step 4: Run the tests and verify they pass**

Run: `cd web && bun test src/stores/use-session-store.test.ts`
Expected: PASS, 7 tests.

Note: `bun test` runs without a DOM, so `localStorage` is undefined and every access falls into the `catch`. That is intentional — the store must work when storage is unavailable, and the tests prove the in-memory transitions independently of it.

- [ ] **Step 5: Commit**

```bash
git add web/src/stores/use-session-store.ts web/src/stores/use-session-store.test.ts
git commit -m "feat(web): hold the Staff Access Session in a client store"
```

---

### Task 3: Normalize transport failures into ApiError

**Files:**
- Modify: `web/src/lib/api-client.ts`
- Create: `web/src/lib/api-client.test.ts`

**Interfaces:**
- Consumes: `ApiError` from Task 1, `useSessionStore` from Task 2.
- Produces: `toApiError(error: unknown): ApiError`, exported for testing; the existing `axiosInstance` and `customAxiosInstance` keep their names and signatures.

The current response interceptor deletes the token from `localStorage` on any 401 and re-throws a raw axios error. Both are replaced: the store owns session lifetime, and callers receive `ApiError`.

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/api-client.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { toApiError } from "./api-client";
import { ApiError } from "./unwrap";

describe("toApiError", () => {
  it("carries the status and the server code", () => {
    const err = toApiError({
      isAxiosError: true,
      response: {
        status: 403,
        data: { success: false, error: { code: "FORBIDDEN", message: "phiên đang bị khóa" } },
      },
    });
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(403);
    expect(err.code).toBe("FORBIDDEN");
    expect(err.message).toBe("phiên đang bị khóa");
  });

  it("represents a transport failure with status 0", () => {
    const err = toApiError({ isAxiosError: true, message: "Network Error" });
    expect(err.status).toBe(0);
    expect(err.code).toBe("NETWORK_ERROR");
  });

  it("passes an ApiError through unchanged", () => {
    const original = new ApiError(429, "TOO_MANY_REQUESTS", "chờ chút");
    expect(toApiError(original)).toBe(original);
  });
});
```

- [ ] **Step 2: Run the test and verify it fails**

Run: `cd web && bun test src/lib/api-client.test.ts`
Expected: FAIL, `toApiError` is not exported.

- [ ] **Step 3: Rewrite the interceptors**

Replace the whole body of `web/src/lib/api-client.ts` with:

```ts
import axios, { type AxiosRequestConfig, type AxiosResponse } from "axios";
import { ApiError } from "./unwrap";
import { useSessionStore } from "@/stores/use-session-store";

export const axiosInstance = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || "/api/v1",
  timeout: 15000,
  headers: {
    "Content-Type": "application/json",
  },
});

axiosInstance.interceptors.request.use((config) => {
  const token = useSessionStore.getState().token;
  if (token && config.headers) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

interface AxiosLikeError {
  response?: { status?: number; data?: { error?: { code?: string; message?: string } } };
  message?: string;
}

/** Normalizes anything axios rejects with into an ApiError. */
export function toApiError(error: unknown): ApiError {
  if (error instanceof ApiError) return error;
  const e = error as AxiosLikeError;
  const status = e.response?.status ?? 0;
  if (status === 0) {
    return new ApiError(0, "NETWORK_ERROR", e.message ?? "Network Error");
  }
  return new ApiError(
    status,
    e.response?.data?.error?.code ?? "UNKNOWN",
    e.response?.data?.error?.message ?? e.message ?? "Đã xảy ra lỗi",
  );
}

axiosInstance.interceptors.response.use(
  (response) => response,
  (error) => {
    const apiError = toApiError(error);
    // 401 means the Staff Access Session is gone. 403 is ambiguous between a
    // locked session and an authority denial, and is resolved in query-client.
    if (apiError.status === 401) {
      useSessionStore.getState().clear();
    }
    return Promise.reject(apiError);
  },
);

export const customAxiosInstance = <T>(
  config: AxiosRequestConfig,
  options?: AxiosRequestConfig,
): Promise<T> => {
  return axiosInstance({
    ...config,
    ...options,
  }).then((response: AxiosResponse<T>) => response.data);
};
```

- [ ] **Step 4: Run the tests and verify they pass**

Run: `cd web && bun test`
Expected: PASS, 18 tests across 4 files.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/api-client.ts web/src/lib/api-client.test.ts
git commit -m "feat(web): normalize transport failures into ApiError"
```

---

### Task 4: The auth API seam

**Files:**
- Create: `web/src/features/auth/api/use-auth.ts`

**Interfaces:**
- Consumes: `unwrap`/`ApiError` (Task 1), `useSessionStore` (Task 2).
- Produces: `fetchSessionState(): Promise<AuthSessionStateResponse>`; hooks `useSignIn()`, `useDeclareWorkspace()`, `useUnlock()`, `useLock()`, `useSignOut()`.

Why this file wraps rather than re-exports: orval generated `useGetAuthSession` as a **mutation**, not a query, because of how the GET is tagged. Route loaders need a plain promise, so this seam calls the generated plain functions directly. It carries no logic of its own beyond `unwrap`, which Task 1 already tests, so it ships without tests of its own.

Endpoint facts the implementer needs (`internal/auth/routes.go`): `/auth/session`, `/auth/sign-in`, `/auth/unlock`, `/auth/bootstrap`, and `/auth/identities` are registered **outside** `RequireAuth`, so `/auth/session` and `/auth/unlock` answer while the session is locked. `/auth/lock`, `/auth/sign-out`, `/auth/workspace`, and `/auth/activity` require an active session. `/auth/sign-in` and `/auth/unlock` are rate limited and return `429`.

- [ ] **Step 1: Write the seam**

Create `web/src/features/auth/api/use-auth.ts`:

```ts
import { useMutation } from "@tanstack/react-query";
import {
  getAuthSession,
  postAuthActivity,
  postAuthLock,
  postAuthSignIn,
  postAuthSignOut,
  postAuthUnlock,
  postAuthWorkspace,
} from "@/api/generated/endpoints/auth/auth";
import type { AuthSessionStateResponse, AuthSignInResponse } from "@/api/generated/models";
import { unwrap, type ApiError } from "@/lib/unwrap";
import { useSessionStore, type Workspace } from "@/stores/use-session-store";

/** Reads the authoritative session state. Answers while locked. */
export async function fetchSessionState(): Promise<AuthSessionStateResponse> {
  return unwrap<AuthSessionStateResponse>(await getAuthSession());
}

export async function pingActivity(): Promise<void> {
  await postAuthActivity();
}

function toProfile(signIn: AuthSignInResponse) {
  const staff = signIn.staff;
  return {
    staffId: staff?.id ?? "",
    displayName: staff?.display_name ?? "",
    loginCode: staff?.login_code ?? "",
    roles: staff?.roles ?? [],
    capabilities: staff?.capabilities ?? [],
  };
}

export function useSignIn() {
  return useMutation<AuthSignInResponse, ApiError, { login_code: string; pin: string }>({
    mutationFn: async (request) => unwrap<AuthSignInResponse>(await postAuthSignIn(request)),
    onSuccess: (data) => {
      useSessionStore.getState().signIn(data.token ?? "", toProfile(data));
    },
  });
}

export function useUnlock() {
  return useMutation<AuthSignInResponse, ApiError, string>({
    mutationFn: async (pin) => unwrap<AuthSignInResponse>(await postAuthUnlock({ pin })),
    onSuccess: (data) => {
      useSessionStore.getState().signIn(data.token ?? "", toProfile(data));
    },
  });
}

export function useDeclareWorkspace() {
  return useMutation<void, ApiError, Workspace>({
    mutationFn: async (workspace) => {
      await postAuthWorkspace({ workspace });
    },
    onSuccess: (_data, workspace) => {
      useSessionStore.getState().setWorkspace(workspace);
    },
  });
}

export function useLock() {
  return useMutation<void, ApiError, void>({
    mutationFn: async () => {
      await postAuthLock();
    },
    onSuccess: () => {
      useSessionStore.getState().lock();
    },
  });
}

export function useSignOut() {
  return useMutation<void, ApiError, void>({
    mutationFn: async () => {
      await postAuthSignOut();
    },
    onSettled: () => {
      // The local session ends even if the server call failed: the operator
      // asked to leave the terminal.
      useSessionStore.getState().clear();
    },
  });
}
```

- [ ] **Step 2: Typecheck**

Run: `cd web && bunx tsc -b`
Expected: no errors. If `AuthSignInResponse.staff` is typed differently than assumed, correct `toProfile` to match the generated model rather than casting.

- [ ] **Step 3: Commit**

```bash
git add web/src/features/auth/api/use-auth.ts
git commit -m "feat(web): add the auth API seam over generated endpoints"
```

---

### Task 5: The PIN pad and the input primitive

**Files:**
- Create: `web/src/features/auth/components/pin-pad.tsx`
- Create: `web/src/components/ui/input.tsx`

**Interfaces:**
- Consumes: `Button` from `@/components/ui/button`.
- Produces: `<PinPad value onChange maxLength? onSubmit? disabled? />`; `<Input />` forwarding `React.ComponentProps<"input">`.

These are presentational, so they carry no unit tests; their behavior is covered by the UAT gate in Task 11. Match the existing primitive conventions: `cn` imported from `"cn"`, `data-slot` attributes, semantic tokens only.

- [ ] **Step 1: Create the input primitive**

Create `web/src/components/ui/input.tsx`:

```tsx
import { cn } from "cn";

function Input({ className, type = "text", ...props }: React.ComponentProps<"input">) {
  return (
    <input
      data-slot="input"
      type={type}
      className={cn(
        "flex h-12 w-full rounded-xl border border-input bg-card px-4 text-base text-foreground outline-none transition-colors",
        "placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50",
        "disabled:pointer-events-none disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}

export { Input };
```

- [ ] **Step 2: Create the PIN pad**

Create `web/src/features/auth/components/pin-pad.tsx`:

```tsx
import { Delete } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "cn";

export interface PinPadProps {
  value: string;
  onChange: (next: string) => void;
  /** The server accepts 4 to 8 digits (internal/auth/domain.go). */
  maxLength?: number;
  onSubmit?: () => void;
  disabled?: boolean;
  className?: string;
}

const DIGITS = ["1", "2", "3", "4", "5", "6", "7", "8", "9"];

export function PinPad({
  value,
  onChange,
  maxLength = 8,
  onSubmit,
  disabled = false,
  className,
}: PinPadProps) {
  const append = (digit: string) => {
    if (value.length >= maxLength) return;
    onChange(value + digit);
  };

  const backspace = () => onChange(value.slice(0, -1));

  return (
    <div className={cn("grid grid-cols-3 gap-3", className)}>
      {DIGITS.map((digit) => (
        <Button
          key={digit}
          type="button"
          variant="outline"
          disabled={disabled}
          className="h-12 font-mono text-xl font-bold"
          onClick={() => append(digit)}
        >
          {digit}
        </Button>
      ))}
      <Button
        type="button"
        variant="outline"
        disabled={disabled || value.length === 0}
        className="h-12"
        onClick={backspace}
        aria-label="Xóa một số"
      >
        <Delete className="size-5" />
      </Button>
      <Button
        type="button"
        variant="outline"
        disabled={disabled}
        className="h-12 font-mono text-xl font-bold"
        onClick={() => append("0")}
      >
        0
      </Button>
      <Button
        type="button"
        variant="default"
        disabled={disabled || value.length < 4}
        className="h-12 font-semibold"
        onClick={onSubmit}
      >
        Vào ca
      </Button>
    </div>
  );
}
```

- [ ] **Step 3: Typecheck**

Run: `cd web && bunx tsc -b`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/ui/input.tsx web/src/features/auth/components/pin-pad.tsx
git commit -m "feat(web): add the touch PIN pad and input primitive"
```

---

### Task 6: Sign-in against the real endpoint

**Files:**
- Modify: `web/src/features/auth/components/login-view.tsx` (full rewrite)
- Modify: `web/src/routes/auth/login.tsx`

**Interfaces:**
- Consumes: `useSignIn` (Task 4), `PinPad` and `Input` (Task 5), `messageForError` (Task 1), `useSessionStore` (Task 2).
- Produces: `<LoginView />` unchanged in name, so the route file's import still resolves.

The current file is a mock: it accepts any PIN, has no login code field, and links to `/` with a "Demo POS" escape hatch. All three go. The visual reference is `design-system/pos-cafe/pages/auth.html`.

- [ ] **Step 1: Rewrite the view**

Replace the whole of `web/src/features/auth/components/login-view.tsx` with:

```tsx
import * as React from "react";
import { Coffee } from "lucide-react";
import { useNavigate } from "@tanstack/react-router";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { PinPad } from "./pin-pad";
import { useSignIn } from "../api/use-auth";
import { messageForError } from "@/lib/error-messages";

export function LoginView() {
  const [loginCode, setLoginCode] = React.useState("");
  const [pin, setPin] = React.useState("");
  const navigate = useNavigate();
  const signIn = useSignIn();

  const canSubmit = loginCode.trim().length > 0 && pin.length >= 4 && !signIn.isPending;

  const submit = () => {
    if (!canSubmit) return;
    signIn.mutate(
      { login_code: loginCode.trim(), pin },
      {
        onSuccess: () => {
          setPin("");
          void navigate({ to: "/auth/workspace" });
        },
        onError: () => setPin(""),
      },
    );
  };

  return (
    <div className="flex min-h-screen w-full select-none items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm border-border shadow-md">
        <CardHeader className="p-6 pb-4 text-center">
          <div className="mx-auto mb-2 flex h-12 w-12 items-center justify-center rounded-xl bg-primary text-primary-foreground">
            <Coffee className="size-6" />
          </div>
          <CardTitle className="text-xl font-bold">Đăng nhập nhân viên</CardTitle>
          <CardDescription className="text-xs text-muted-foreground">
            Nhập mã nhân viên và mã PIN để mở phiên làm việc
          </CardDescription>
        </CardHeader>

        <CardContent className="flex flex-col gap-4 p-6 pt-0">
          <Input
            value={loginCode}
            onChange={(e) => setLoginCode(e.target.value.toUpperCase())}
            maxLength={24}
            autoFocus
            placeholder="Mã nhân viên, ví dụ TN01"
            className="font-mono tracking-wider"
            aria-label="Mã nhân viên"
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
          />

          <div className="flex h-12 w-full items-center justify-center rounded-xl border border-input bg-muted/40 font-mono text-2xl tracking-widest text-foreground">
            {pin ? (
              "•".repeat(pin.length)
            ) : (
              <span className="font-sans text-xs text-muted-foreground">Mã PIN 4 đến 8 số</span>
            )}
          </div>

          <PinPad value={pin} onChange={setPin} onSubmit={submit} disabled={signIn.isPending} />

          {signIn.isError && (
            <p role="alert" className="text-center text-sm text-destructive">
              {messageForError(signIn.error)}
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd web && bunx tsc -b`
Expected: one error, `/auth/workspace` is not a known route. Task 7 creates it. If you want a green typecheck before then, do Task 7 before running the build.

- [ ] **Step 3: Commit**

```bash
git add web/src/features/auth/components/login-view.tsx
git commit -m "feat(web): sign in against the real auth endpoint"
```

---

### Task 7: Workspace declaration

**Files:**
- Create: `web/src/features/auth/components/workspace-picker.tsx`
- Create: `web/src/routes/auth/workspace.tsx`

**Interfaces:**
- Consumes: `useDeclareWorkspace` (Task 4), `rememberedWorkspace` and `useSessionStore` (Task 2), `messageForError` (Task 1).
- Produces: `<WorkspacePicker />`; the route `/auth/workspace`.

Why this screen exists at all: `CONTEXT.md` defines Staff Workspace as what the session's inactivity lock policy depends on — 5 minutes at a counter station, 15 minutes on a preparation display (`internal/auth/domain.go`). Declaring it is a business act, not a preference.

- [ ] **Step 1: Create the picker**

Create `web/src/features/auth/components/workspace-picker.tsx`:

```tsx
import { ChefHat, ShieldCheck, ShoppingCart } from "lucide-react";
import { useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useDeclareWorkspace } from "../api/use-auth";
import { rememberedWorkspace, useSessionStore, type Workspace } from "@/stores/use-session-store";
import { messageForError } from "@/lib/error-messages";

const CHOICES: Array<{
  value: Workspace;
  label: string;
  hint: string;
  icon: typeof ShoppingCart;
}> = [
  { value: "cashier", label: "Quầy thu ngân", hint: "Tự khóa sau 5 phút", icon: ShoppingCart },
  { value: "preparation", label: "Khu pha chế", hint: "Tự khóa sau 15 phút", icon: ChefHat },
  { value: "manager", label: "Quản lý", hint: "Tự khóa sau 5 phút", icon: ShieldCheck },
];

export function WorkspacePicker() {
  const navigate = useNavigate();
  const declare = useDeclareWorkspace();
  const displayName = useSessionStore((s) => s.displayName);
  const remembered = rememberedWorkspace();

  const choose = (workspace: Workspace) => {
    declare.mutate(workspace, {
      onSuccess: () => void navigate({ to: "/" }),
    });
  };

  return (
    <div className="flex min-h-screen w-full select-none items-center justify-center bg-background p-4">
      <Card className="w-full max-w-md border-border shadow-md">
        <CardHeader className="p-6 pb-4">
          <CardTitle className="text-xl font-bold">Chào {displayName ?? "bạn"}</CardTitle>
          <CardDescription className="text-xs text-muted-foreground">
            Chọn khu vực làm việc cho phiên này. Khu vực quyết định thời gian tự khóa màn hình.
          </CardDescription>
        </CardHeader>

        <CardContent className="flex flex-col gap-3 p-6 pt-0">
          {CHOICES.map(({ value, label, hint, icon: Icon }) => (
            <Button
              key={value}
              variant={value === remembered ? "default" : "outline"}
              disabled={declare.isPending}
              className="h-12 w-full justify-start gap-3 px-4 text-base font-semibold"
              onClick={() => choose(value)}
            >
              <Icon className="size-5" />
              <span className="flex-1 text-left">{label}</span>
              <span className="text-xs font-normal opacity-70">{hint}</span>
            </Button>
          ))}

          {declare.isError && (
            <p role="alert" className="text-center text-sm text-destructive">
              {messageForError(declare.error)}
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
```

- [ ] **Step 2: Create the route**

Create `web/src/routes/auth/workspace.tsx`:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { WorkspacePicker } from "@/features/auth/components/workspace-picker";

export const Route = createFileRoute("/auth/workspace")({
  component: WorkspacePicker,
});
```

- [ ] **Step 3: Typecheck and verify the route tree regenerates**

Run: `cd web && bunx tsc -b`
Expected: no errors. `src/routeTree.gen.ts` is regenerated by the router plugin on the next `bun run dev` or build; if the typecheck still reports `/auth/workspace` as unknown, run `bun run build` once to regenerate it.

- [ ] **Step 4: Commit**

```bash
git add web/src/features/auth/components/workspace-picker.tsx web/src/routes/auth/workspace.tsx web/src/routeTree.gen.ts
git commit -m "feat(web): declare the Staff Workspace after sign in"
```

---

### Task 8: Session hydration and route guards

**Files:**
- Create: `web/src/lib/guards.ts`
- Modify: `web/src/routes/__root.tsx`
- Modify: `web/src/routes/_app.tsx`
- Modify: `web/src/routes/_app/index.tsx`, `web/src/routes/_app/kds.tsx`, `web/src/routes/_app/shift.tsx`, `web/src/routes/_app/settings.tsx`

**Interfaces:**
- Consumes: `fetchSessionState` (Task 4), `useSessionStore` (Task 2), `ApiError` (Task 1).
- Produces: `hydrateSession(): Promise<void>`, `requireAuthenticated(): void`, `requireCapability(capability: string): void`.

`_app` has no guard today. That is harmless only while its children are placeholders; from this slice on they call real APIs.

Capability per route, chosen from the verified strings in Global Constraints: `/` needs `sales.operate`; `/shift` needs `sales_shift.operate`; `/kds` needs `preparation.operate`; `/settings` needs `staff.administer`. `/tables` and `/history` get authentication only — their read capabilities are not yet settled in the Go slices, and guessing one here would lock out a role that the server would have allowed. Their own slices tighten them.

- [ ] **Step 1: Write the guards**

Create `web/src/lib/guards.ts`:

```ts
import { redirect } from "@tanstack/react-router";
import { useSessionStore } from "@/stores/use-session-store";
import { fetchSessionState } from "@/features/auth/api/use-auth";

/**
 * Asks the server what the session is and records the answer.
 *
 * The token in localStorage is a cache, never the authority: only the server
 * knows whether a session is revoked, expired, or locked for inactivity.
 */
export async function hydrateSession(): Promise<void> {
  const store = useSessionStore.getState();
  if (!store.token) {
    store.clear();
    return;
  }
  try {
    store.applyServerState(await fetchSessionState());
  } catch {
    store.clear();
  }
}

export function requireAuthenticated(): void {
  const { state } = useSessionStore.getState();
  if (state === "signed_out") {
    throw redirect({ to: "/auth/login" });
  }
  // A locked session stays on the route; the lock overlay covers it.
}

export function requireCapability(capability: string): void {
  requireAuthenticated();
  const { state, capabilities } = useSessionStore.getState();
  // Capability is not evaluated while locked: the overlay is showing and the
  // operator has not yet proven they are still there.
  if (state === "authenticated" && !capabilities.includes(capability)) {
    throw redirect({ to: "/" });
  }
}
```

- [ ] **Step 2: Hydrate at the root**

Replace `web/src/routes/__root.tsx` with:

```tsx
import { createRootRoute, Outlet } from "@tanstack/react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/lib/query-client";
import { hydrateSession } from "@/lib/guards";

export const Route = createRootRoute({
  beforeLoad: async () => {
    await hydrateSession();
  },
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

- [ ] **Step 3: Guard the shell**

In `web/src/routes/_app.tsx`, add the import and the `beforeLoad`:

```tsx
import { createFileRoute, Outlet } from "@tanstack/react-router";
import { PosHeader } from "@/components/layout/pos-header";
import { requireAuthenticated } from "@/lib/guards";

export const Route = createFileRoute("/_app")({
  beforeLoad: () => requireAuthenticated(),
  component: AppLayout,
});
```

Leave `AppLayout` as it is; Task 9 changes it.

- [ ] **Step 4: Guard the four capability-bound routes**

In each of the four route files, add the import and the `beforeLoad` line, keeping the existing `component`:

`web/src/routes/_app/index.tsx`:

```tsx
import { requireCapability } from "@/lib/guards";
// ...inside createFileRoute("/_app/"):
  beforeLoad: () => requireCapability("sales.operate"),
```

`web/src/routes/_app/shift.tsx`:

```tsx
import { requireCapability } from "@/lib/guards";
// ...inside createFileRoute("/_app/shift"):
  beforeLoad: () => requireCapability("sales_shift.operate"),
```

`web/src/routes/_app/kds.tsx`:

```tsx
import { requireCapability } from "@/lib/guards";
// ...inside createFileRoute("/_app/kds"):
  beforeLoad: () => requireCapability("preparation.operate"),
```

`web/src/routes/_app/settings.tsx`:

```tsx
import { requireCapability } from "@/lib/guards";
// ...inside createFileRoute("/_app/settings"):
  beforeLoad: () => requireCapability("staff.administer"),
```

- [ ] **Step 5: Typecheck**

Run: `cd web && bunx tsc -b`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/guards.ts web/src/routes/__root.tsx web/src/routes/_app.tsx web/src/routes/_app/index.tsx web/src/routes/_app/shift.tsx web/src/routes/_app/kds.tsx web/src/routes/_app/settings.tsx
git commit -m "feat(web): hydrate the session and guard routes by capability"
```

---

### Task 9: Lock, unlock, and activity

**Files:**
- Create: `web/src/features/auth/components/lock-overlay.tsx`
- Create: `web/src/hooks/use-activity-ping.ts`
- Modify: `web/src/lib/query-client.ts`
- Modify: `web/src/routes/_app.tsx`
- Modify: `web/src/components/layout/pos-header.tsx`

**Interfaces:**
- Consumes: `useUnlock`, `useLock`, `useSignOut`, `pingActivity`, `fetchSessionState` (Task 4); `PinPad` (Task 5); `useSessionStore` (Task 2); `messageForError` (Task 1).
- Produces: `<LockOverlay />`; `useActivityPing(): void`.

The central fact: `internal/auth/middleware.go` returns the **same** `403 FORBIDDEN` for a locked session and for a missing role, differing only in a Vietnamese message. Never branch on message text. On any `403`, re-read `GET /auth/session` — registered outside `RequireAuth`, so it answers while locked — and let its `state` decide.

- [ ] **Step 1: Resolve 403 globally**

Replace `web/src/lib/query-client.ts` with:

```ts
import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { ApiError } from "./unwrap";
import { useSessionStore } from "@/stores/use-session-store";
import { fetchSessionState } from "@/features/auth/api/use-auth";

/**
 * A 403 is ambiguous: internal/auth/middleware.go returns it both for a locked
 * session and for a missing role. Only the server can tell them apart, so ask.
 */
async function resolveForbidden(error: unknown): Promise<void> {
  if (!(error instanceof ApiError) || error.status !== 403) return;
  try {
    const state = await fetchSessionState();
    if (state.state === "locked") {
      useSessionStore.getState().lock();
    } else if (state.state === "signed_out") {
      useSessionStore.getState().clear();
    }
  } catch {
    // Leave the session as it is; the next request will try again.
  }
}

export const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError: (error) => void resolveForbidden(error) }),
  mutationCache: new MutationCache({ onError: (error) => void resolveForbidden(error) }),
  defaultOptions: {
    queries: {
      staleTime: 1000 * 60 * 2,
      retry: 1,
      refetchOnWindowFocus: false,
    },
    mutations: {
      retry: 0,
    },
  },
});
```

- [ ] **Step 2: Create the lock overlay**

Create `web/src/features/auth/components/lock-overlay.tsx`:

```tsx
import * as React from "react";
import { Lock } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { PinPad } from "./pin-pad";
import { useSignOut, useUnlock } from "../api/use-auth";
import { useSessionStore } from "@/stores/use-session-store";
import { messageForError } from "@/lib/error-messages";

export function LockOverlay() {
  const [pin, setPin] = React.useState("");
  const displayName = useSessionStore((s) => s.displayName);
  const loginCode = useSessionStore((s) => s.loginCode);
  const unlock = useUnlock();
  const signOut = useSignOut();

  const submit = () => {
    if (pin.length < 4 || unlock.isPending) return;
    unlock.mutate(pin, {
      onSuccess: () => setPin(""),
      onError: () => setPin(""),
    });
  };

  return (
    <div className="fixed inset-0 z-50 flex select-none items-center justify-center bg-background/95 p-4 backdrop-blur-sm">
      <Card className="w-full max-w-sm border-border shadow-lg">
        <CardHeader className="p-6 pb-4 text-center">
          <div className="mx-auto mb-2 flex h-12 w-12 items-center justify-center rounded-xl bg-muted text-muted-foreground">
            <Lock className="size-6" />
          </div>
          <CardTitle className="text-xl font-bold">Màn hình đã khóa</CardTitle>
          <CardDescription className="text-xs text-muted-foreground">
            {displayName ?? "Nhân viên"}
            {loginCode ? ` · ${loginCode}` : ""} — nhập PIN để tiếp tục ca đang làm
          </CardDescription>
        </CardHeader>

        <CardContent className="flex flex-col gap-4 p-6 pt-0">
          <div className="flex h-12 w-full items-center justify-center rounded-xl border border-input bg-muted/40 font-mono text-2xl tracking-widest text-foreground">
            {pin ? "•".repeat(pin.length) : <span className="font-sans text-xs text-muted-foreground">Mã PIN</span>}
          </div>

          <PinPad value={pin} onChange={setPin} onSubmit={submit} disabled={unlock.isPending} />

          {unlock.isError && (
            <p role="alert" className="text-center text-sm text-destructive">
              {messageForError(unlock.error)}
            </p>
          )}

          <Button
            variant="ghost"
            className="h-12 text-sm text-muted-foreground"
            disabled={signOut.isPending}
            onClick={() => signOut.mutate()}
          >
            Đăng xuất khỏi thiết bị này
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
```

- [ ] **Step 3: Create the activity ping**

Create `web/src/hooks/use-activity-ping.ts`:

```ts
import * as React from "react";
import { pingActivity } from "@/features/auth/api/use-auth";
import { useSessionStore } from "@/stores/use-session-store";

const THROTTLE_MS = 30_000;
const EVENTS = ["pointerdown", "keydown"] as const;

/**
 * Reports real human activity so the server can extend the inactivity window.
 *
 * The server owns the lock policy — 5 minutes for cashier and manager, 15 for
 * preparation. The client never decides to lock, and never reports activity
 * that a human did not cause.
 */
export function useActivityPing(): void {
  const state = useSessionStore((s) => s.state);
  const lastSent = React.useRef(0);

  React.useEffect(() => {
    if (state !== "authenticated") return;

    const onActivity = () => {
      const now = Date.now();
      if (now - lastSent.current < THROTTLE_MS) return;
      lastSent.current = now;
      void pingActivity().catch(() => {
        // A failed ping is not worth surfacing; the next interaction retries.
      });
    };

    for (const event of EVENTS) {
      window.addEventListener(event, onActivity, { passive: true });
    }
    return () => {
      for (const event of EVENTS) {
        window.removeEventListener(event, onActivity);
      }
    };
  }, [state]);
}
```

- [ ] **Step 4: Mount both in the shell**

Replace `AppLayout` in `web/src/routes/_app.tsx` with:

```tsx
function AppLayout() {
  const state = useSessionStore((s) => s.state);
  useActivityPing();

  return (
    <div className="flex h-screen w-screen flex-col overflow-hidden bg-background text-foreground">
      <PosHeader />
      <main className="flex-1 overflow-hidden">
        <Outlet />
      </main>
      {state === "locked" && <LockOverlay />}
    </div>
  );
}
```

and add to its imports:

```tsx
import { useSessionStore } from "@/stores/use-session-store";
import { useActivityPing } from "@/hooks/use-activity-ping";
import { LockOverlay } from "@/features/auth/components/lock-overlay";
```

- [ ] **Step 5: Put identity and the lock button in the header**

In `web/src/components/layout/pos-header.tsx`, replace any hardcoded staff name or placeholder identity with live store values, and add two actions on the right-hand side:

```tsx
import { Lock, LogOut } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useSessionStore } from "@/stores/use-session-store";
import { useLock, useSignOut } from "@/features/auth/api/use-auth";

// inside the component body:
const displayName = useSessionStore((s) => s.displayName);
const workspace = useSessionStore((s) => s.workspace);
const lock = useLock();
const signOut = useSignOut();

const WORKSPACE_LABELS: Record<string, string> = {
  cashier: "Quầy thu ngân",
  preparation: "Khu pha chế",
  manager: "Quản lý",
};

// inside the rendered header, on the right:
<div className="flex items-center gap-2">
  <div className="text-right">
    <div className="text-sm font-semibold text-foreground">{displayName ?? "—"}</div>
    <div className="text-2xs text-muted-foreground">
      {workspace ? WORKSPACE_LABELS[workspace] : "Chưa chọn khu vực"}
    </div>
  </div>
  <Button
    variant="outline"
    size="icon-lg"
    aria-label="Khóa màn hình"
    disabled={lock.isPending}
    onClick={() => lock.mutate()}
  >
    <Lock className="size-5" />
  </Button>
  <Button
    variant="ghost"
    size="icon-lg"
    aria-label="Đăng xuất"
    disabled={signOut.isPending}
    onClick={() => signOut.mutate()}
  >
    <LogOut className="size-5" />
  </Button>
</div>
```

- [ ] **Step 6: Typecheck and run the suite**

Run: `cd web && bunx tsc -b && bun test`
Expected: no type errors; 18 tests pass.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/auth/components/lock-overlay.tsx web/src/hooks/use-activity-ping.ts web/src/lib/query-client.ts web/src/routes/_app.tsx web/src/components/layout/pos-header.tsx
git commit -m "feat(web): lock, unlock, and report activity"
```

---

### Task 10: The development seed

**Files:**
- Create: `scripts/dev-seed.ts`
- Modify: `Makefile`

**Interfaces:**
- Consumes: a running API on `http://localhost:8080`.
- Produces: `make dev-seed`.

Without this, slice 1 cannot be acceptance-tested at all: a clean database holds no Manager, so there is nobody to sign in as. `POST /auth/bootstrap` creates the first Manager and is registered outside `RequireAuth`.

This is a development script. Phase 11 acceptance forbids production configuration from exposing demo identities, so it never runs as part of a build or release.

- [ ] **Step 1: Read the bootstrap request shape**

Run: `sed -n '/auth.BootstrapManagerRequest:/,/^  auth\./p' docs/swagger.yaml`
Use the exact field names it prints in the next step rather than the ones assumed here.

- [ ] **Step 2: Write the seed script**

Create `scripts/dev-seed.ts`:

```ts
/**
 * Development seed. Creates the first Manager so the UI can be signed into.
 *
 * Never run against production: Phase 11 acceptance forbids production
 * configuration from exposing demo identities or sample sales.
 *
 * Usage: bun run scripts/dev-seed.ts
 */
const BASE = process.env.POS_API_BASE ?? "http://localhost:8080/api/v1";

const MANAGER = {
  display_name: "Quan Ly Demo",
  login_code: "QL01",
  pin: "1234",
};

async function post(path: string, body: unknown): Promise<unknown> {
  const res = await fetch(`${BASE}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const payload = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(`${path} -> ${res.status} ${JSON.stringify(payload)}`);
  }
  return payload;
}

async function main(): Promise<void> {
  try {
    await post("/auth/bootstrap", MANAGER);
    console.log(`Bootstrapped manager ${MANAGER.login_code} with PIN ${MANAGER.pin}`);
  } catch (error) {
    console.log(`Bootstrap skipped or failed, continuing: ${String(error)}`);
  }
  console.log("Sign in at http://localhost:5173/auth/login");
}

await main();
```

- [ ] **Step 3: Add the Makefile target**

Append to `Makefile`, following the existing `##` help-comment convention:

```makefile
dev-seed: ## Seed a development Manager identity against a running API (never for production)
	cd web && bun run ../scripts/dev-seed.ts
```

- [ ] **Step 4: Verify it runs**

Run, with PostgreSQL up (`make docker-up`) and the API running (`make run`):

```bash
make dev-seed
```

Expected: either "Bootstrapped manager QL01 with PIN 1234", or the skip message if a Manager already exists. If bootstrap rejects the payload, correct the field names from Step 1 and rerun.

- [ ] **Step 5: Commit**

```bash
git add scripts/dev-seed.ts Makefile
git commit -m "chore: seed a development manager identity"
```

---

### Task 11: The UAT gate

**Files:** none.

This slice is not done until the operator confirms it. Do not self-certify, do not merge, do not start slice 2.

- [x] **Step 1: Verify the mechanical checks yourself first**

```bash
cd web && bunx tsc -b && bun test && bun run lint
```
Expected: no type errors, all tests pass, lint clean. Report the real output; if something fails, say so rather than describing the slice as ready.

- [x] **Step 2: Start the stack**

```bash
make docker-up
make run          # in one terminal
cd web && bun run dev   # in another
make dev-seed     # once, if the database is fresh
```

- [x] **Step 3: Hand over this script and stop**

Ask the operator to walk it and confirm:

1. Open `http://localhost:5173`. Expect a redirect to `/auth/login`, not a POS screen.
2. Sign in with a wrong PIN. Expect a Vietnamese error under the keypad and the PIN field cleared, with the login code kept.
3. Press the wrong PIN repeatedly. Expect the rate-limit message rather than a generic failure.
4. Sign in correctly as `QL01` / `1234`. Expect the workspace screen.
5. Choose "Quầy thu ngân". Expect the POS shell, with the staff name and "Quầy thu ngân" in the header.
6. Press the lock button. Expect the overlay, with the staff name and login code shown.
7. Enter a wrong PIN on the overlay, then the correct one. Expect an error, then the same screen back — not a fresh login and not a lost route.
8. Refresh the page while signed in. Expect to stay signed in and on the same screen.
9. Sign in as a Cashier identity, if one exists, and navigate to `/settings`. Expect a redirect to `/`, because `staff.administer` is Manager-only.
10. Stop the Go API and press something. Expect the "không kết nối được máy chủ" message, never a silently queued action.

- [x] **Step 4: Record the outcome**

Outcome: Operator confirmed and approved UAT gate. Slice 1 implementation complete.

---

## Self-Review

**Spec coverage.** §3 definition of done: Tasks 4 through 9 call the real API (1); error states render through `messageForError` (2); guards in Task 8 (3); `request_id` is spec §4.5's deferred item, not applicable to any auth command (4); the prototype is followed in Tasks 5 through 9 (5); basic tests only, Tasks 1 through 3 (6); the UAT gate is Task 11 (7); commits are per task, and the PR opens after Task 11 (8). §4.1 seam: Task 4. §4.2 `ApiError` and the code table: Tasks 1 and 3; the `403` disambiguation: Task 9 Step 1. §4.4 server state on the server: no query cache is duplicated into the store; the store holds only session facts. §4.5 primitives: session store Task 2, `unwrap` Task 1, guards Task 8; `command.ts` and `ManagerApprovalDialog` are explicitly deferred to slice 2 and are named in Global Constraints so an implementer does not add them. §5 all three surfaces: Tasks 6, 7, 9. §7 seed gap: Task 10.

**Type consistency.** `SessionState`, `Workspace`, and `StaffProfile` are defined in Task 2 and used under those names in Tasks 3, 4, 7, 8, and 9. `unwrap`, `ApiError`, `ApiEnvelope` are defined in Task 1 and consumed in Tasks 3, 4, and 9. `hasCapability` is the store method; `requireCapability` is the router guard — different names for different layers, deliberately. `fetchSessionState` and `pingActivity` are defined in Task 4 and consumed in Tasks 8 and 9.

**Shapes verified against the generated models, not assumed.** `AuthSignInResponse` is `{staff?: AuthStaffProfileResponse, token?: string}` and `AuthStaffProfileResponse` carries `id`, `display_name`, `login_code`, `roles`, `capabilities` — so `toProfile` in Task 4 matches. `auth.BootstrapManagerRequest` requires exactly `display_name` (2 to 120 characters), `login_code` (2 to 24), and `pin` (4 to 8) — so the Task 10 payload matches. `AuthSessionStateResponse.state` is typed as an open `string`, which is why Task 2's `applyServerState` treats anything other than `authenticated` or `locked` as signed out. The verification steps in Task 4 Step 2 and Task 10 Step 1 remain as a guard against drift after the next `bun run codegen`.
