import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { CorrectionsLog } from "./corrections-log";

const base = { busy: false, onRemake: () => {} };

describe("CorrectionsLog", () => {
  it("renders nothing when there are no entries", () => {
    expect(renderToString(<CorrectionsLog {...base} corrections={[]} />)).toBe("");
  });

  it("shows a Waste entry with a Remake action", () => {
    const html = renderToString(
      <CorrectionsLog
        {...base}
        corrections={[
          { id: "w1", entry_kind: "WASTE", item_name: "Trà đào", unit_number: 1, service_number: "020" },
        ]}
      />,
    );
    expect(html).toContain("Trà đào");
    expect(html).toContain('aria-label="Pha lại món"');
  });

  it("shows a Remake entry with no Remake action of its own", () => {
    const html = renderToString(
      <CorrectionsLog
        {...base}
        corrections={[
          {
            id: "r1",
            entry_kind: "REMAKE",
            item_name: "Trà đào",
            unit_number: 2,
            service_number: "020",
            waste_id: "w1",
            source_preparation_unit_id: "u1",
            source_unit_number: 1,
          },
        ]}
      />,
    );
    expect(html).not.toContain('aria-label="Pha lại món"');
  });

  it("suppresses the Remake action when the Waste already has a matching Remake entry", () => {
    const html = renderToString(
      <CorrectionsLog
        {...base}
        corrections={[
          { id: "w1", entry_kind: "WASTE", item_name: "Trà đào", unit_number: 1, service_number: "020" },
          {
            id: "r1",
            entry_kind: "REMAKE",
            item_name: "Trà đào",
            unit_number: 2,
            service_number: "020",
            waste_id: "w1",
            source_preparation_unit_id: "u1",
            source_unit_number: 1,
          },
        ]}
      />,
    );
    expect(html).not.toContain('aria-label="Pha lại món"');
  });
});
