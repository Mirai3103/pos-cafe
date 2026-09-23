import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { AlertsPanel } from "./alerts-panel";

const base = { busy: false, onAcknowledge: () => {} };

describe("AlertsPanel", () => {
  it("renders nothing when there are no alerts", () => {
    expect(renderToString(<AlertsPanel {...base} alerts={[]} />)).toBe("");
  });

  it("shows each alert's translated kind and reason, item, unit and service number", () => {
    const html = renderToString(
      <AlertsPanel
        {...base}
        alerts={[
          {
            id: "al1",
            kind: "WASTE",
            item_name: "Bạc xỉu đá",
            unit_number: 2,
            service_number: "014",
            reason: "QUALITY_FAILURE",
          },
        ]}
      />,
    );
    expect(html).toContain("Đã huỷ do lỗi/hết");
    expect(html).toContain("Bạc xỉu đá");
    expect(html).toContain("#2");
    expect(html).toContain("014");
    expect(html).toContain("Không đạt chất lượng");
    expect(html).not.toContain("QUALITY_FAILURE");
  });

  it("uses Vietnamese fallbacks for unknown alert values", () => {
    const html = renderToString(
      <AlertsPanel
        {...base}
        alerts={[
          {
            id: "al1",
            kind: "NEW_KIND",
            item_name: "Trà đào",
            unit_number: 1,
            service_number: "020",
            reason: "NEW_REASON",
          },
        ]}
      />,
    );
    expect(html).toContain("Cảnh báo chuẩn bị");
    expect(html).toContain("Lý do khác");
    expect(html).not.toContain("NEW_KIND");
    expect(html).not.toContain("NEW_REASON");
  });
});
