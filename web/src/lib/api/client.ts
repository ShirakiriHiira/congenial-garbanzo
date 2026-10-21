import { apiErrorForStatus, ApiError, isAbortError } from "./errors";
import type { Decoder } from "./decode";
import { failSession, session } from "../session.svelte";
import { getInitData } from "../telegram/sdk";

const API_PREFIX = "/api/v1";
const REQUEST_TIMEOUT_MS = 10_000;
const activeRequests = new Set<AbortController>();

interface RequestOptions {
  method?: "GET" | "POST" | "PATCH" | "DELETE";
  body?: unknown;
  signal?: AbortSignal;
}

interface ResponseData<T> {
  status: number;
  data: T;
}

function abortInFlightRequests(except?: AbortController): void {
  for (const controller of activeRequests) {
    if (controller !== except) {
      controller.abort();
    }
  }
}

function parseErrorBody(value: unknown): { code?: string; message?: string } {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    return {};
  }
  const error = (value as Record<string, unknown>).error;
  if (error === null || typeof error !== "object" || Array.isArray(error)) {
    return {};
  }
  const source = error as Record<string, unknown>;
  return {
    code: typeof source.code === "string" ? source.code : undefined,
    message: typeof source.message === "string" ? source.message : undefined,
  };
}

function assertExactIntegers(value: unknown, path: string): void {
  if (typeof value === "number") {
    if (!Number.isSafeInteger(value)) {
      throw new RangeError(`${path} must be a safe integer to be sent exactly.`);
    }
    return;
  }
  if (Array.isArray(value)) {
    value.forEach((entry, index) => assertExactIntegers(entry, `${path}[${index}]`));
    return;
  }
  if (value !== null && typeof value === "object") {
    for (const [key, nested] of Object.entries(value)) {
      assertExactIntegers(nested, `${path}.${key}`);
    }
  }
}

async function readJSON(response: Response): Promise<unknown | undefined> {
  if (response.status === 204) {
    return undefined;
  }
  const contentType = response.headers.get("content-type")?.toLowerCase() ?? "";
  if (!contentType.includes("application/json")) {
    return undefined;
  }
  try {
    return await response.json();
  } catch {
    return undefined;
  }
}

export async function request<T>(
  path: string,
  options: RequestOptions,
  decode: Decoder<T>,
): Promise<ResponseData<T>> {
  if (session.auth === "expired" || session.auth === "forbidden") {
    throw apiErrorForStatus(session.auth === "expired" ? 401 : 403);
  }

  const initData = getInitData();
  if (initData.length === 0) {
    throw apiErrorForStatus(401, "unauthorized", "Open this Mini App from Telegram to continue.");
  }
  if (options.body !== undefined) {
    assertExactIntegers(options.body, "body");
  }

  const controller = new AbortController();
  let timedOut = false;
  const onCallerAbort = () => controller.abort();
  options.signal?.addEventListener("abort", onCallerAbort, { once: true });
  if (options.signal?.aborted) {
    controller.abort();
  }
  const timeout = window.setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, REQUEST_TIMEOUT_MS);
  activeRequests.add(controller);

  try {
    const headers = new Headers({
      Accept: "application/json",
      Authorization: `tma ${initData}`,
    });
    const request: RequestInit = {
      method: options.method ?? "GET",
      headers,
      signal: controller.signal,
      cache: "no-store",
    };
    if (options.body !== undefined) {
      headers.set("Content-Type", "application/json");
      request.body = JSON.stringify(options.body);
    }

    const response = await fetch(`${API_PREFIX}${path}`, request);
    const body = await readJSON(response);
    if (!response.ok) {
      const parsed = parseErrorBody(body);
      const error = apiErrorForStatus(response.status, parsed.code, parsed.message);
      if (response.status === 401 || response.status === 403) {
        failSession(response.status);
        abortInFlightRequests(controller);
      }
      throw error;
    }

    try {
      return { status: response.status, data: decode(body) };
    } catch (error) {
      if (isAbortError(error)) {
        throw error;
      }
      throw new ApiError("server", "The server returned an invalid response.", response.status, "invalid_response");
    }
  } catch (error) {
    if (error instanceof ApiError) {
      throw error;
    }
    if (timedOut) {
      throw new ApiError("timeout", "The request timed out.");
    }
    if (isAbortError(error)) {
      throw error;
    }
    if (controller.signal.aborted || options.signal?.aborted) {
      throw new DOMException("The request was cancelled.", "AbortError");
    }
    throw new ApiError("network", "The network request failed.");
  } finally {
    window.clearTimeout(timeout);
    options.signal?.removeEventListener("abort", onCallerAbort);
    activeRequests.delete(controller);
  }
}
