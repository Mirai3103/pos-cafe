import { describe, it, expect } from "bun:test";
import { renderToString } from "react-dom/server";
import { PaymentDialog } from "./payment-dialog";

const base = {
  isOpen: true,
  serviceNumber: "007",
  totalVnd: 47_000,
  isCommitted: false,
  isSubmitting: false,
  errorMessage: null,
  changeDueVnd: null,
  submitStatus: "idle" as const,
  submitError: null,
  onClose: () => {},
  onConfirm: () => {},
  onDone: () => {},
};

describe("PaymentDialog", () => {
  it("renders nothing while closed", () => {
    expect(renderToString(<PaymentDialog {...base} isOpen={false} />)).toBe("");
  });

  it("shows the total and the quick tender buttons", () => {
    const html = renderToString(<PaymentDialog {...base} />);
    expect(html).toContain("47.000");
    expect(html).toContain("Đúng tiền");
    expect(html).toContain("50.000");
    expect(html).toContain("100.000");
  });

  it("labels the confirm action for an uncommitted draft", () => {
    const html = renderToString(<PaymentDialog {...base} />);
    expect(html).toContain("Xác nhận");
  });

  it("announces the committed state when the draft is already charged", () => {
    const html = renderToString(<PaymentDialog {...base} isCommitted />);
    expect(html).toContain("Đã chốt đơn");
  });

  it("shows the change on the result screen", () => {
    const html = renderToString(<PaymentDialog {...base} changeDueVnd={3_000} />);
    expect(html).toContain("Tiền thối");
    expect(html).toContain("3.000");
    expect(html).toContain("Xong");
  });

  it("surfaces an error message", () => {
    const html = renderToString(
      <PaymentDialog {...base} errorMessage="Đơn chưa có món nào để thanh toán." />,
    );
    expect(html).toContain("Đơn chưa có món nào để thanh toán.");
  });

  it("reports the order sent to the bar on the result screen", () => {
    const html = renderToString(
      <PaymentDialog {...base} changeDueVnd={3_000} submitStatus="submitted" />,
    );
    expect(html).toContain("Đã gửi bếp");
  });

  it("reports a submit in flight", () => {
    const html = renderToString(
      <PaymentDialog {...base} changeDueVnd={3_000} submitStatus="submitting" />,
    );
    expect(html).toContain("Đang gửi bếp");
  });

  it("keeps a submit failure apart from the payment", () => {
    const html = renderToString(
      <PaymentDialog
        {...base}
        changeDueVnd={3_000}
        submitStatus="failed"
        submitError="Không kết nối được máy chủ. Kiểm tra mạng nội bộ rồi thử lại."
      />,
    );
    expect(html).toContain("Tiền thối");
    expect(html).toContain("Đã thu tiền nhưng chưa gửi được bếp");
    expect(html).toContain("Không kết nối được máy chủ");
  });
});
