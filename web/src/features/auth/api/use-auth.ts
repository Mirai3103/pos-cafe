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
