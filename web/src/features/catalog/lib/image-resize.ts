// web/src/features/catalog/lib/image-resize.ts
import { ERROR_MESSAGES } from "@/lib/error-messages";
import { ApiError } from "@/lib/unwrap";

export const MAX_IMAGE_EDGE = 800;
/** The server's cap (BA-1 §3.2). */
export const MAX_IMAGE_BYTES = 1_048_576;
export const QUALITY_STEPS = [0.85, 0.75, 0.65, 0.55, 0.45] as const;

export function fitWithin(width: number, height: number, maxEdge = MAX_IMAGE_EDGE): { width: number; height: number } {
  const scale = Math.min(1, maxEdge / Math.max(width, height));
  return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)) };
}

/**
 * Shrinks a picked image in the browser so no image library enters the Go
 * binary. A browser without WebP encoding returns PNG, which the server also
 * accepts.
 */
export async function resizeToWebp(file: Blob): Promise<Blob> {
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file);
  } catch {
    throw new ApiError(0, "INVALID_IMAGE", ERROR_MESSAGES.INVALID_IMAGE);
  }
  const { width, height } = fitWithin(bitmap.width, bitmap.height);
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new ApiError(0, "INVALID_IMAGE", ERROR_MESSAGES.INVALID_IMAGE);
  ctx.drawImage(bitmap, 0, 0, width, height);
  bitmap.close();
  for (const quality of QUALITY_STEPS) {
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/webp", quality));
    if (!blob) break;
    if (blob.size <= MAX_IMAGE_BYTES) return blob;
  }
  throw new ApiError(0, "IMAGE_TOO_LARGE", ERROR_MESSAGES.IMAGE_TOO_LARGE);
}
