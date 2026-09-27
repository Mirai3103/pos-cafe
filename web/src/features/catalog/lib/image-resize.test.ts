// web/src/features/catalog/lib/image-resize.test.ts
import { describe, expect, it } from "bun:test";
import { fitWithin, MAX_IMAGE_BYTES, MAX_IMAGE_EDGE } from "./image-resize";

describe("fitWithin", () => {
  it("scales the long edge down to the cap and keeps the ratio", () => {
    expect(fitWithin(1600, 1200)).toEqual({ width: 800, height: 600 });
    expect(fitWithin(1000, 3000)).toEqual({ width: 267, height: 800 });
  });

  it("never upscales", () => {
    expect(fitWithin(400, 300)).toEqual({ width: 400, height: 300 });
  });

  it("uses the BA-1 limits", () => {
    expect(MAX_IMAGE_EDGE).toBe(800);
    expect(MAX_IMAGE_BYTES).toBe(1_048_576);
  });
});
