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

  it("refuses to lock when there is no token, because the unlock surface would be unsatisfiable", () => {
    useSessionStore.getState().lock();
    expect(useSessionStore.getState().state).toBe("signed_out");
  });

  it("refuses a live server state when the store holds no token", () => {
    useSessionStore.getState().applyServerState({
      state: "authenticated",
      staff_id: "staff-1",
      display_name: "Nguyen Thu Ngan",
      login_code: "TN01",
      roles: ["CASHIER"],
      capabilities: ["sales.operate"],
    });
    const s = useSessionStore.getState();
    expect(s.state).toBe("signed_out");
    expect(s.token).toBeNull();
    expect(s.capabilities).toEqual([]);
  });
});
