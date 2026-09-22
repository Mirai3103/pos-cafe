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

  it("rejects previous promise with MANAGER_APPROVAL_SUPERSEDED if promptApproval is called again", async () => {
    const store = useManagerApprovalStore.getState();
    const firstPromise = store.promptApproval({
      title: "Yêu cầu 1",
      description: "Thao tác 1",
    });

    const secondPromise = store.promptApproval({
      title: "Yêu cầu 2",
      description: "Thao tác 2",
    });

    await expect(firstPromise).rejects.toThrow("MANAGER_APPROVAL_SUPERSEDED");

    useManagerApprovalStore.getState().confirm({
      approverLoginCode: "MGR02",
      managerPin: "9999",
    });

    const secondResult = await secondPromise;
    expect(secondResult.approverLoginCode).toBe("MGR02");
    expect(secondResult.managerPin).toBe("9999");
    expect(useManagerApprovalStore.getState().isOpen).toBe(false);
  });
});
