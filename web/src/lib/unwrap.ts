export interface ApiEnvelope<T> {
  success?: boolean;
  data?: T;
  error?: { code?: string; message?: string };
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

/**
 * Narrows a Go API envelope to its payload.
 *
 * Kept out of the axios mutator on purpose: unwrapping there would make the
 * orval-generated types describe a runtime shape that no longer exists.
 */
export function unwrap<T>(res: ApiEnvelope<T>): T {
  if (res.success === false || res.error) {
    throw new ApiError(0, res.error?.code ?? "UNKNOWN", res.error?.message ?? "Đã xảy ra lỗi");
  }
  if (res.data === undefined || res.data === null) {
    throw new ApiError(0, "EMPTY_RESPONSE", "Máy chủ trả về dữ liệu rỗng");
  }
  return res.data;
}
