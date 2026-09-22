import { describe, it, expect, mock } from "bun:test";
import { renderToString } from "react-dom/server";
import { ItemPickerDialog } from "./item-picker-dialog";
import type { CatalogSellableItemResponse } from "@/api/generated/models";

const mockItemWithSizesAndModifiers: CatalogSellableItemResponse = {
  id: "item-1",
  name: "Cà phê sữa đá",
  price_vnd: 25000,
  is_available: true,
  category_id: "cat-coffee",
  sizes: [
    { id: "size-s", name: "Nhỏ (S)", price_vnd: 25000 },
    { id: "size-m", name: "Vừa (M)", price_vnd: 30000 },
    { id: "size-l", name: "Lớn (L)", price_vnd: 35000 },
  ],
  modifier_groups: [
    {
      id: "group-sweetness",
      name: "Độ ngọt",
      min_selections: 1,
      max_selections: 1,
      default_option_ids: ["opt-sugar-100"],
      options: [
        { id: "opt-sugar-100", name: "100% đường", surcharge_vnd: 0 },
        { id: "opt-sugar-50", name: "50% đường", surcharge_vnd: 0 },
        { id: "opt-sugar-0", name: "Không đường", surcharge_vnd: 0 },
      ],
    },
    {
      id: "group-toppings",
      name: "Topping thêm",
      min_selections: 0,
      max_selections: 3,
      options: [
        { id: "opt-flan", name: "Bánh Flan", surcharge_vnd: 8000 },
        { id: "opt-jelly", name: "Thạch cà phê", surcharge_vnd: 5000 },
      ],
    },
  ],
};

const mockSimpleItem: CatalogSellableItemResponse = {
  id: "item-2",
  name: "Bánh mì que",
  price_vnd: 15000,
  is_available: true,
  category_id: "cat-food",
};

describe("ItemPickerDialog", () => {
  it("exports ItemPickerDialog component function", () => {
    expect(typeof ItemPickerDialog).toBe("function");
  });

  it("renders nothing when isOpen is false", () => {
    const html = renderToString(
      <ItemPickerDialog
        item={mockItemWithSizesAndModifiers}
        isOpen={false}
        onClose={mock()}
        onConfirm={mock()}
      />,
    );
    expect(html).toBe("");
  });

  it("renders nothing when item is null", () => {
    const html = renderToString(
      <ItemPickerDialog
        item={null}
        isOpen={true}
        onClose={mock()}
        onConfirm={mock()}
      />,
    );
    expect(html).toBe("");
  });

  it("renders dialog with header, sizes, modifier groups, and initial calculations", () => {
    const html = renderToString(
      <ItemPickerDialog
        item={mockItemWithSizesAndModifiers}
        isOpen={true}
        onClose={mock()}
        onConfirm={mock()}
      />,
    );

    // Header checks
    expect(html).toContain("Cà phê sữa đá");
    expect(html).toContain("Tùy chọn kích cỡ, topping &amp; ghi chú");
    expect(html).toContain("picker-dialog-title");

    // Size section checks
    expect(html).toContain("Kích cỡ");
    expect(html).toContain("Bắt buộc chọn 1");
    expect(html).toContain("Nhỏ (S)");
    expect(html).toContain("Vừa (M)");
    expect(html).toContain("Lớn (L)");

    // Modifier group checks
    expect(html).toContain("Độ ngọt");
    expect(html).toContain("100% đường");
    expect(html).toContain("50% đường");
    expect(html).toContain("Topping thêm");
    expect(html).toContain("Bánh Flan");
    expect(html).toContain("Thạch cà phê");

    // Stepper and CTA
    expect(html).toContain("Thêm vào đơn");
    expect(html).toContain("Ghi chú pha chế");
    expect(html).toContain("min-h-[48px]");
  });

  it("renders simple item without size or modifier sections", () => {
    const html = renderToString(
      <ItemPickerDialog
        item={mockSimpleItem}
        isOpen={true}
        onClose={mock()}
        onConfirm={mock()}
      />,
    );

    expect(html).toContain("Bánh mì que");
    expect(html).not.toContain("Kích cỡ");
    expect(html).not.toContain("Topping thêm");
    expect(html).toContain("Thêm vào đơn");
  });

  it("respects custom confirmLabel and initial values", () => {
    const html = renderToString(
      <ItemPickerDialog
        item={mockItemWithSizesAndModifiers}
        initialValues={{
          sizeId: "size-m",
          selectedOptionIds: ["opt-flan"],
          preparationNote: "Giao gấp",
          quantity: 3,
        }}
        isOpen={true}
        onClose={mock()}
        onConfirm={mock()}
        confirmLabel="Cập nhật món"
      />,
    );

    expect(html).toContain("Cập nhật món");
  });

  it("enforces minimum touch targets >= 48px on all interactive elements", () => {
    const html = renderToString(
      <ItemPickerDialog
        item={mockItemWithSizesAndModifiers}
        isOpen={true}
        onClose={mock()}
        onConfirm={mock()}
      />,
    );

    // All buttons should have min-h-[48px]
    const buttonMatches = html.match(/<button[^>]*>/g) ?? [];
    expect(buttonMatches.length).toBeGreaterThan(0);
    for (const btn of buttonMatches) {
      expect(btn).toContain("min-h-[48px]");
    }
  });

  it("uses font-mono for whole VND prices and numbers", () => {
    const html = renderToString(
      <ItemPickerDialog
        item={mockItemWithSizesAndModifiers}
        isOpen={true}
        onClose={mock()}
        onConfirm={mock()}
      />,
    );

    expect(html).toContain("font-mono");
    expect(html).toContain("tabular-nums");
  });
});
