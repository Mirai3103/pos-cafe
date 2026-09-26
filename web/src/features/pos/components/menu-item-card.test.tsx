import { describe, it, expect } from "bun:test";
import { renderToString } from "react-dom/server";

import { MenuItemCard } from "./menu-item-card";
import type { CatalogSellableItemResponse } from "@/api/generated/models";

const baseItem: CatalogSellableItemResponse = {
  id: "item-1",
  category_id: "cat-1",
  name: "Cà phê sữa đá",
  price_vnd: 29000,
  available: true,
};

const noop = () => {};

describe("MenuItemCard media well", () => {
  it("renders the item image from the API and never the placeholder service", () => {
    const html = renderToString(
      <MenuItemCard
        item={{ ...baseItem, image_url: "/media/catalog/abc.jpg" }}
        onSelect={noop}
      />,
    );
    expect(html).toContain('src="/media/catalog/abc.jpg"');
    expect(html).not.toContain("placewaifu");
  });

  it("falls back to the icon well when the item has no image", () => {
    const html = renderToString(<MenuItemCard item={baseItem} onSelect={noop} />);
    expect(html).not.toContain("<img");
    expect(html).toContain("lucide-coffee");
  });

  it("renders the item name", () => {
    const html = renderToString(<MenuItemCard item={baseItem} onSelect={noop} />);
    expect(html).toContain("Cà phê sữa đá");
  });
});
