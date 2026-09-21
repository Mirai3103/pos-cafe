import axios, { type AxiosRequestConfig, type AxiosResponse } from "axios";
import { ApiError } from "./unwrap";
import { useSessionStore } from "@/stores/use-session-store";

export const axiosInstance = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || "/api/v1",
  timeout: 15000,
  headers: {
    "Content-Type": "application/json",
  },
});

axiosInstance.interceptors.request.use((config) => {
  const token = useSessionStore.getState().token;
  if (token && config.headers) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

interface AxiosLikeError {
  response?: { status?: number; data?: { error?: { code?: string; message?: string } } };
  message?: string;
}

/** Normalizes anything axios rejects with into an ApiError. */
export function toApiError(error: unknown): ApiError {
  if (error instanceof ApiError) return error;
  const e = error as AxiosLikeError;
  const status = e.response?.status ?? 0;
  if (status === 0) {
    return new ApiError(0, "NETWORK_ERROR", e.message ?? "Network Error");
  }
  return new ApiError(
    status,
    e.response?.data?.error?.code ?? "UNKNOWN",
    e.response?.data?.error?.message ?? e.message ?? "Đã xảy ra lỗi",
  );
}

axiosInstance.interceptors.response.use(
  (response) => response,
  (error) => {
    const apiError = toApiError(error);
    // 401 means the Staff Access Session is gone. 403 is ambiguous between a
    // locked session and an authority denial, and is resolved in query-client.
    if (apiError.status === 401) {
      useSessionStore.getState().clear();
    }
    return Promise.reject(apiError);
  },
);

export const customAxiosInstance = <T>(
  config: AxiosRequestConfig,
  options?: AxiosRequestConfig,
): Promise<T> => {
  return axiosInstance({
    ...config,
    ...options,
  }).then((response: AxiosResponse<T>) => response.data);
};
