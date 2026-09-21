import { create } from "zustand";

export interface ManagerApprovalRequest {
  title: string;
  description: string;
  confirmLabel?: string;
}

export interface ManagerApprovalCredentials {
  approverLoginCode: string;
  managerPin: string;
}

export interface ManagerApprovalState {
  isOpen: boolean;
  request: ManagerApprovalRequest | null;
  resolve: ((creds: ManagerApprovalCredentials) => void) | null;
  reject: ((error: Error) => void) | null;
  promptApproval: (request: ManagerApprovalRequest) => Promise<ManagerApprovalCredentials>;
  confirm: (creds: ManagerApprovalCredentials) => void;
  cancel: () => void;
}

export const useManagerApprovalStore = create<ManagerApprovalState>((set, get) => ({
  isOpen: false,
  request: null,
  resolve: null,
  reject: null,

  promptApproval: (request) => {
    return new Promise<ManagerApprovalCredentials>((resolve, reject) => {
      set({
        isOpen: true,
        request,
        resolve,
        reject,
      });
    });
  },

  confirm: (creds) => {
    const { resolve } = get();
    if (resolve) resolve(creds);
    set({ isOpen: false, request: null, resolve: null, reject: null });
  },

  cancel: () => {
    const { reject } = get();
    if (reject) reject(new Error("MANAGER_APPROVAL_CANCELLED"));
    set({ isOpen: false, request: null, resolve: null, reject: null });
  },
}));
