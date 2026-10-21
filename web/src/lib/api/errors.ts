export type ApiErrorKind =
  | "auth"
  | "forbidden"
  | "validation"
  | "not-found"
  | "conflict"
  | "payload"
  | "unsupported-media"
  | "server"
  | "unavailable"
  | "network"
  | "timeout"
  | "pagination";

export class ApiError extends Error {
  readonly status?: number;
  readonly code?: string;
  readonly kind: ApiErrorKind;

  constructor(kind: ApiErrorKind, message: string, status?: number, code?: string) {
    super(message);
    this.name = "ApiError";
    this.kind = kind;
    this.status = status;
    this.code = code;
  }
}

export function apiErrorForStatus(status: number, code?: string, message?: string): ApiError {
  const kind: ApiErrorKind =
    status === 400
      ? "validation"
      : status === 401
        ? "auth"
        : status === 403
          ? "forbidden"
          : status === 404
            ? "not-found"
            : status === 409
              ? "conflict"
              : status === 413
                ? "payload"
                : status === 415
                  ? "unsupported-media"
                  : status === 503
                    ? "unavailable"
                    : "server";
  const fallback =
    kind === "auth"
      ? "Authentication is missing or expired."
      : kind === "forbidden"
        ? "Access is denied."
        : kind === "validation"
          ? "The request is invalid."
          : kind === "not-found"
            ? "The requested resource was not found."
            : kind === "conflict"
              ? "The resource has changed or conflicts with its current state."
              : kind === "payload"
                ? "The request body is too large."
                : kind === "unsupported-media"
                  ? "The request body must be JSON."
                  : kind === "unavailable"
                    ? "The service is temporarily unavailable."
                    : "The service could not complete the request.";
  return new ApiError(kind, message || fallback, status, code);
}

export function isAbortError(value: unknown): boolean {
  return value instanceof DOMException && value.name === "AbortError";
}
