import { useCallback, useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  getGetStaffQueryKey,
  patchStaffId,
  patchStaffIdEnabled,
  postStaff,
  postStaffIdResetPin,
  useGetStaff,
} from "@/api/generated/endpoints/staff/staff";
import type { GetStaff200 } from "@/api/generated/models";
import { fetchSessionState } from "@/features/auth/api/use-auth";
import { newRequestId } from "@/lib/command";
import { playErrorBuzz, playSuccessChirp } from "@/lib/sound";
import { ApiError, unwrap, unwrapNullable } from "@/lib/unwrap";
import { useManagerApprovalStore, type ManagerApprovalRequest } from "@/stores/use-manager-approval-store";
import { useSessionStore } from "@/stores/use-session-store";
import { toStaffRows, type StaffForm, type StaffRow } from "../lib/staff";

const selectRows = (res: GetStaff200) => toStaffRows(unwrapNullable(res));

export function useStaffList() {
  const query = useGetStaff<StaffRow[], ApiError>({ query: { select: selectRows } });
  return {
    rows: query.data ?? [],
    isPending: query.isPending,
    error: query.data ? null : query.error,
    refetch: () => void query.refetch(),
  };
}

export type CommandResult = { status: "done" } | { status: "cancelled" } | { status: "failed"; error: ApiError };

const toApiError = (err: unknown): ApiError =>
  err instanceof ApiError ? err : new ApiError(0, "UNKNOWN", err instanceof Error ? err.message : "Đã xảy ra lỗi");

/**
 * Every staff command: ask for the Manager PIN, send once with a fresh
 * request_id, then refetch the list whether it worked or not.
 */
export function useStaffCommands() {
  const queryClient = useQueryClient();

  const run = useCallback(
    async (approval: ManagerApprovalRequest, send: (pin: string) => Promise<unknown>): Promise<CommandResult> => {
      let pin: string;
      try {
        pin = (await useManagerApprovalStore.getState().promptApproval(approval)).managerPin;
      } catch {
        return { status: "cancelled" };
      }
      try {
        await send(pin);
        playSuccessChirp();
        return { status: "done" };
      } catch (err) {
        playErrorBuzz();
        return { status: "failed", error: toApiError(err) };
      } finally {
        await queryClient.invalidateQueries({ queryKey: getGetStaffQueryKey() });
      }
    },
    [queryClient],
  );

  return useMemo(
    () => ({
      create: (form: StaffForm) =>
        run({ title: "Thêm nhân viên", description: `Tạo tài khoản cho ${form.displayName.trim()}.`, confirmLabel: "Xác nhận & Tạo" }, async (pin) =>
          unwrap(
            await postStaff({
              request_id: newRequestId(),
              display_name: form.displayName.trim(),
              login_code: form.loginCode.trim().toUpperCase(),
              roles: form.roles,
              pin: form.pin,
              enabled: form.enabled,
              manager_pin: pin,
            }),
          ),
        ),

      update: (row: StaffRow, form: StaffForm) =>
        run({ title: "Lưu thông tin nhân viên", description: `Cập nhật ${row.displayName}.`, confirmLabel: "Xác nhận & Lưu" }, async (pin) => {
          unwrap(
            await patchStaffId(row.id, {
              request_id: newRequestId(),
              display_name: form.displayName.trim(),
              login_code: form.loginCode.trim().toUpperCase(),
              roles: form.roles,
              manager_pin: pin,
            }),
          );
          // Editing yourself changes the header name and the stored login code.
          if (row.id === useSessionStore.getState().staffId) {
            useSessionStore.getState().applyServerState(await fetchSessionState());
          }
        }),

      resetPin: (row: StaffRow, newPin: string) =>
        run({ title: "Đặt lại PIN", description: `${row.displayName} sẽ bị đăng xuất trên mọi thiết bị.`, confirmLabel: "Xác nhận & Đặt lại" }, async (pin) =>
          unwrap(await postStaffIdResetPin(row.id, { request_id: newRequestId(), pin: newPin, manager_pin: pin })),
        ),

      setEnabled: (row: StaffRow, enabled: boolean) =>
        run(
          {
            title: enabled ? "Mở khóa tài khoản" : "Khóa tài khoản",
            description: enabled ? `${row.displayName} có thể đăng nhập lại.` : `${row.displayName} sẽ bị đăng xuất trên mọi thiết bị.`,
            confirmLabel: enabled ? "Xác nhận & Mở khóa" : "Xác nhận & Khóa",
          },
          async (pin) =>
            unwrap(
              await patchStaffIdEnabled(row.id, {
                request_id: newRequestId(),
                expected_enabled: row.enabled,
                enabled,
                manager_pin: pin,
              }),
            ),
        ),
    }),
    [run],
  );
}
