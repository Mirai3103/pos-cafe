import { describe, expect, it } from "bun:test";
import { useManagerApprovalStore } from "./use-manager-approval-store";

describe("useManagerApprovalStore", () => {
  it("starts in closed state with null request", () => {
    const state = useManagerApprovalStore.getState();
    expect(state.isOpen).toBe(false);
    expect(state.request).toBeNull();
  });

  it("opens modal and resolves promise on confirm", async () => {
    const store = useManagerApprovalStore.getState();
    const promptPromise = store.promptApproval({
      title: "Xác nhận duyệt",
      description: "Yêu cầu mã PIN Quản lý",
    });

    expect(useManagerApprovalStore.getState().isOpen).toBe(true);
    expect(useManagerApprovalStore.getState().request?.title).toBe("Xác nhận duyệt");

    useManagerApprovalStore.getState().confirm({
      approverLoginCode: "MGR01",
      managerPin: "1234",
    });

    const result = await promptPromise;
    expect(result.approverLoginCode).toBe("MGR01");
    expect(result.managerPin).toBe("1234");
    expect(useManagerApprovalStore.getState().isOpen).toBe(false);
  });

  it("rejects promise on cancel", async () => {
    const store = useManagerApprovalStore.getState();
    const promptPromise = store.promptApproval({
      title: "Huỷ thao tác",
      description: "Thao tác huỷ",
    });

    useManagerApprovalStore.getState().cancel();

    expect(promptPromise).rejects.toThrow("MANAGER_APPROVAL_CANCELLED");
    expect(useManagerApprovalStore.getState().isOpen).toBe(false);
  });
});
