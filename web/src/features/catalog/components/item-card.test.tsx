// web/src/features/catalog/components/item-card.test.tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { formatVND } from "@/lib/utils";
import type { CatItem } from "../lib/catalog-model";
import { ItemCard } from "./item-card";

const item: CatItem = {
  id: "i",
  categoryId: "c",
  name: "Cà phê sữa đá",
  code: null,
  badge: "BEST_SELLER",
  description: null,
  imageUrl: "/media/catalog/a.webp",
  priceVnd: 29000,
  sizes: [],
  directGroupIds: [],
  excludedGroupIds: [],
};

describe("ItemCard", () => {
  it("shows the image, derived code, badge, price, and group count", () => {
    const html = renderToString(<ItemCard item={item} categoryName="Cà phê" groupCount={2} onOpen={() => {}} />);
    expect(html).toContain("/media/catalog/a.webp");
    expect(html).toContain("cfsd");
    expect(html).toContain("Bán chạy");
    expect(html).toContain(formatVND(29000));
    expect(html).toContain("2 nhóm topping");
  });
});
