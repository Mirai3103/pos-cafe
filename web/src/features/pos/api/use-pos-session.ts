import * as React from "react";
import { useServiceSession, useStartTakeawaySession } from "./use-pos";
import type { SalesServiceSessionResponse } from "@/api/generated/models";

const STORAGE_SESSION_KEY = "pos_active_session_id";

export interface PosSessionHandle {
  activeSessionId: string | null;
  session: SalesServiceSessionResponse | null;
  isSessionError: boolean;
  ensureSessionId: () => Promise<string>;
  clearSession: () => void;
}

function readStoredSessionId(): string | null {
  try {
    return sessionStorage.getItem(STORAGE_SESSION_KEY);
  } catch {
    return null;
  }
}

function writeStoredSessionId(id: string | null): void {
  try {
    if (id === null) sessionStorage.removeItem(STORAGE_SESSION_KEY);
    else sessionStorage.setItem(STORAGE_SESSION_KEY, id);
  } catch {
    // ignore storage errors
  }
}

/**
 * Owns the single active Service Session pointer for this browser tab.
 *
 * The Session is opened lazily, on the first item added, so browsing the menu
 * never leaves an empty Session behind.
 */
export function usePosSession(): PosSessionHandle {
  const [activeSessionId, setActiveSessionId] = React.useState<string | null>(
    readStoredSessionId,
  );

  const activeSessionIdRef = React.useRef<string | null>(activeSessionId);
  React.useEffect(() => {
    activeSessionIdRef.current = activeSessionId;
  }, [activeSessionId]);

  const creatingSessionPromiseRef = React.useRef<Promise<string> | null>(null);

  const { data: session, isError: isSessionError } = useServiceSession(activeSessionId);
  const { startTakeaway } = useStartTakeawaySession();

  const clearSession = React.useCallback(() => {
    writeStoredSessionId(null);
    activeSessionIdRef.current = null;
    setActiveSessionId(null);
  }, []);

  // Drop the pointer when the Session is gone or no longer active.
  React.useEffect(() => {
    if ((session && session.state && session.state !== "ACTIVE") || isSessionError) {
      writeStoredSessionId(null);
      activeSessionIdRef.current = null;
      // oxlint-disable-next-line react/set-state-in-effect
      setActiveSessionId(null);
    }
  }, [session, isSessionError]);

  const ensureSessionId = React.useCallback(async (): Promise<string> => {
    if (activeSessionIdRef.current) return activeSessionIdRef.current;
    if (creatingSessionPromiseRef.current) return await creatingSessionPromiseRef.current;

    const promise = (async () => {
      try {
        const newSession = await startTakeaway();
        if (!newSession.id) {
          throw new Error("Không thể khởi tạo phiên phục vụ: thiếu mã phiên");
        }
        const id = newSession.id;
        writeStoredSessionId(id);
        activeSessionIdRef.current = id;
        setActiveSessionId(id);
        return id;
      } finally {
        creatingSessionPromiseRef.current = null;
      }
    })();

    creatingSessionPromiseRef.current = promise;
    return await promise;
  }, [startTakeaway]);

  return {
    activeSessionId,
    session: session ?? null,
    isSessionError,
    ensureSessionId,
    clearSession,
  };
}
