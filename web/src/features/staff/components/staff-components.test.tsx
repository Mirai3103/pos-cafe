import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { emptyForm, type StaffRow } from "../lib/staff";
import { ResetPinPanel } from "./reset-pin-modal";
import { StaffFormPanel } from "./staff-form-modal";
import { StaffTable } from "./staff-table";
import { SIGN_OUT_WARNING, ToggleEnabledPanel } from "./toggle-enabled-dialog";

const noop = () => {};
const me: StaffRow = { id: "me", displayName: "Lan", loginCode: "LAN01", enabled: true, roles: ["MANAGER"] };
const other: StaffRow = { id: "o", displayName: "Minh", loginCode: "MINH", enabled: true, roles: ["CASHIER", "BARISTA"] };
const tableProps = { selfId: "me", onEdit: noop, onResetPin: noop, onToggle: noop };

describe("StaffTable", () => {
  it("marks your own row and offers it only Sửa", () => {
    const html = renderToString(<StaffTable rows={[me]} {...tableProps} />);
    expect(html).toContain("Bạn");
    expect(html).toContain("Sửa");
    expect(html).not.toContain("Đặt lại PIN");
    expect(html).not.toContain("Khóa");
  });

  it("offers every action on another row, with role labels", () => {
    const html = renderToString(<StaffTable rows={[other]} {...tableProps} />);
    for (const text of ["Sửa", "Đặt lại PIN", "Khóa", "Thu ngân", "Pha chế", "Hoạt động"]) expect(html).toContain(text);
  });

  it("offers Mở khóa on a disabled row", () => {
    const html = renderToString(<StaffTable rows={[{ ...other, enabled: false }]} {...tableProps} />);
    expect(html).toContain("Mở khóa");
    expect(html).toContain("Đã khóa");
  });

  it("shows the empty state", () => {
    expect(renderToString(<StaffTable rows={[]} {...tableProps} />)).toContain("Không có nhân viên phù hợp");
  });
});

const formProps = { errors: {}, formError: null, busy: false, canSave: true, onChange: noop, onSubmit: noop, onClose: noop };

describe("StaffFormPanel", () => {
  it("asks for a PIN only when creating", () => {
    expect(renderToString(<StaffFormPanel mode="create" form={emptyForm()} managerLocked={false} {...formProps} />)).toContain("Xác nhận PIN");
    expect(renderToString(<StaffFormPanel mode="edit" form={emptyForm()} managerLocked={false} {...formProps} />)).not.toContain("Xác nhận PIN");
  });

  it("locks the Manager checkbox on your own row", () => {
    const html = renderToString(
      <StaffFormPanel mode="edit" form={{ ...emptyForm(), roles: ["MANAGER"] }} managerLocked {...formProps} />,
    );
    expect(html).toMatch(/<input[^>]*disabled[^>]*aria-label="Quản lý"|<input[^>]*aria-label="Quản lý"[^>]*disabled/);
  });

  it("shows field errors", () => {
    const html = renderToString(
      <StaffFormPanel mode="edit" form={emptyForm()} managerLocked={false} {...formProps} errors={{ loginCode: "Mã đăng nhập đã được sử dụng" }} />,
    );
    expect(html).toContain("Mã đăng nhập đã được sử dụng");
  });
});

describe("ResetPinPanel and ToggleEnabledPanel", () => {
  const common = { busy: false, error: null, onClose: noop, onConfirm: noop };

  it("warns that resetting signs the person out", () => {
    const html = renderToString(<ResetPinPanel name="Minh" pin="" pinConfirm="" errors={{}} onChange={noop} {...common} />);
    expect(html).toContain("Minh");
    expect(html).toContain(SIGN_OUT_WARNING);
  });

  it("warns on disable, not on enable", () => {
    expect(renderToString(<ToggleEnabledPanel name="Minh" enabling={false} {...common} />)).toContain(SIGN_OUT_WARNING);
    expect(renderToString(<ToggleEnabledPanel name="Minh" enabling {...common} />)).not.toContain(SIGN_OUT_WARNING);
  });
});
