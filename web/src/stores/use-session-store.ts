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
    // optimistically assumed to be a working session. A live state is also
    // refused when the store holds no token: without it no request can carry
    // credentials, and honoring the state would resurrect a dead session
    // (e.g. a stale 403 resolution landing after sign-out).
    if (
      (serverState.state !== "authenticated" && serverState.state !== "locked") ||
      !get().token
    ) {
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

  // Lock suspends authority; it does not end the Staff Access Session, so the
  // token and identity survive it. Locking without a token is refused: the
  // unlock surface would be unsatisfiable.
  lock: () => {
    if (get().token) set({ state: "locked" });
  },

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
