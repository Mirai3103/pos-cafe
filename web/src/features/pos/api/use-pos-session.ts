import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { getGetSalesServiceSessionsIdQueryKey } from "@/api/generated/endpoints/sales/sales";
import { ApiError } from "@/lib/unwrap";
import { needsNewRound } from "../utils/dine-in";
import { useServiceSession, useStartNextDraft, useStartTakeawaySession } from "./use-pos";
import type { SalesServiceSessionResponse } from "@/api/generated/models";

const STORAGE_SESSION_KEY = "pos_active_session_id";

export interface PosSessionHandle {
  activeSessionId: string | null;
  session: SalesServiceSessionResponse | null;
  isSessionError: boolean;
  ensureSessionId: () => Promise<string>;
  clearSession: () => void;
  switchSession: (id: string) => void;
  ensureDraft: (sessionId: string) => Promise<void>;
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
 * Points this tab's POS at a Session before navigating to it. The floor view
 * uses it to hand a seated party to the cashier terminal.
 */
export function selectPosSession(id: string): void {
  writeStoredSessionId(id);
}

/**
 * Whether the stored pointer no longer names a session this terminal can show.
 * A CLOSED session is kept: the close flow reads back its Completed Sale and
 * clears the pointer when the cashier dismisses it.
 */
export function shouldDropSessionPointer(
  session: SalesServiceSessionResponse | null | undefined,
  isError: boolean,
): boolean {
  if (isError) return true;
  const state = session?.state;
  return Boolean(state) && state !== "ACTIVE" && state !== "CLOSED";
}

/**
 * NEW_ORDER_DRAFT_NOT_AVAILABLE after a refetch: another terminal may already
 * have opened the round, in which case the add can go ahead.
 */
export function recoverAfterRoundConflict(
  projection: SalesServiceSessionResponse | null | undefined,
): boolean {
  return projection?.draft?.state === "EDITABLE";
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

  const queryClient = useQueryClient();
  const { startNextDraft } = useStartNextDraft();
  const sessionRef = React.useRef(session);
  React.useEffect(() => {
    sessionRef.current = session;
  }, [session]);
  const openingRoundRef = React.useRef<Promise<void> | null>(null);

  /**
   * A dine-in Session between rounds has no editable draft; the first item of
   * the next round opens one. Deduplicated like the lazy takeaway Session, so
   * a burst of taps opens one round, and never run otherwise, so sending a
   * round never leaves an empty draft behind.
   */
  const ensureDraft = React.useCallback(
    async (sessionId: string): Promise<void> => {
      const current = sessionRef.current;
      if (!current || current.id !== sessionId || !needsNewRound(current)) return;
      if (openingRoundRef.current) return await openingRoundRef.current;

      const promise = (async () => {
        try {
          await startNextDraft(sessionId);
        } catch (err) {
          if (!(err instanceof ApiError && err.code === "NEW_ORDER_DRAFT_NOT_AVAILABLE")) throw err;
          const key = getGetSalesServiceSessionsIdQueryKey(sessionId);
          await queryClient.refetchQueries({ queryKey: key });
          const cached = queryClient.getQueryData<{ data?: SalesServiceSessionResponse }>(key);
          if (!recoverAfterRoundConflict(cached?.data)) throw err;
        } finally {
          openingRoundRef.current = null;
        }
      })();

      openingRoundRef.current = promise;
      return await promise;
    },
    [startNextDraft, queryClient],
  );

  const clearSession = React.useCallback(() => {
    writeStoredSessionId(null);
    activeSessionIdRef.current = null;
    setActiveSessionId(null);
  }, []);

  /** Reopens a session picked from the pending orders drawer. */
  const switchSession = React.useCallback((id: string) => {
    writeStoredSessionId(id);
    activeSessionIdRef.current = id;
    setActiveSessionId(id);
  }, []);

  // Drop the pointer when the Session is gone or in a state this terminal cannot show.
  React.useEffect(() => {
    if (shouldDropSessionPointer(session, isSessionError)) {
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
    switchSession,
    ensureDraft,
  };
}
