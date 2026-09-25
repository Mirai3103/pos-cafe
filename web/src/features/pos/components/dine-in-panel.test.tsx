import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { DineInPanel } from "./dine-in-panel";
import { deriveDineInStatus } from "../utils/dine-in";
import type { SalesServiceSessionResponse } from "@/api/generated/models";

const session = {
  id: "s1",
  service_number: "012",
  service_mode: "DINE_IN",
  state: "ACTIVE",
  tables: [{ id: "t1", name: "Bàn 1" }],
  draft: { state: "EDITABLE", items: [] },
  checks: [
    {
      id: "c1",
      state: "OPEN",
      charge_vnd: 30_000,
      balance_vnd: 30_000,
      allocations: [{ id: "a1", name: "Bạc xỉu", allocated_quantity: 1, amount_vnd: 30_000, submitted: true }],
      payments: [],
    },
  ],
  orders: [{ id: "o1" }],
  preparation_units: [{ id: "u1", state: "QUEUED" }],
} as SalesServiceSessionResponse;

const noop = () => {};

describe("DineInPanel", () => {
  it("shows the Tables, the sent items, and the balance owed", () => {
    const html = renderToString(
      <DineInPanel
        session={session}
        status={deriveDineInStatus(session)}
        isShiftOpen
        sendError={null}
        isSending={false}
        isClosing={false}
        onEditItem={noop}
        onQuantityChange={noop}
        onRemoveItem={noop}
        onSend={noop}
        onCollect={noop}
        onClose={noop}
        onLeave={noop}
        onChangeTables={noop}
      />,
    );
    expect(html).toContain("Bàn 1");
    expect(html).toContain("#012");
    expect(html).toContain("Bạc xỉu");
    expect(html).toContain("Đã gửi bếp");
    expect(html).toContain("0/1");
  });

  it("never lists a committed-but-unsubmitted allocation as already sent to the kitchen", () => {
    const withUnsubmittedRound = {
      ...session,
      checks: [
        {
          id: "c1",
          state: "OPEN",
          charge_vnd: 60_000,
          balance_vnd: 60_000,
          allocations: [
            { id: "a1", name: "Bạc xỉu", allocated_quantity: 1, amount_vnd: 30_000, submitted: true },
            { id: "a2", name: "Trà đào", allocated_quantity: 1, amount_vnd: 30_000, submitted: false },
          ],
          payments: [],
        },
      ],
    } as SalesServiceSessionResponse;

    const html = renderToString(
      <DineInPanel
        session={withUnsubmittedRound}
        status={deriveDineInStatus(withUnsubmittedRound)}
        isShiftOpen
        sendError={null}
        isSending={false}
        isClosing={false}
        onEditItem={noop}
        onQuantityChange={noop}
        onRemoveItem={noop}
        onSend={noop}
        onCollect={noop}
        onClose={noop}
        onLeave={noop}
        onChangeTables={noop}
      />,
    );
    expect(html).toContain("Đã gửi bếp");
    expect(html).toContain("Bạc xỉu");
    expect(html).not.toContain("Trà đào");
  });
});
