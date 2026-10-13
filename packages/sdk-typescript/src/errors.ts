import type { Item } from "./types.js";

/** Body of an error response: `{"error": {code, message, fields?, details?}}`. */
export interface ErrorBody {
  code: string;
  message: string;
  fields?: Record<string, string>;
  details?: Record<string, unknown>;
}

/** Base class of every error thrown by the SDK. */
export class StaqlyError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options);
    this.name = new.target.name;
  }
}

/** The API answered with a non-2xx status. Subclasses narrow it by status. */
export class APIError extends StaqlyError {
  /** HTTP status code. */
  readonly status: number;
  /** Machine-readable error code, e.g. `not_found`, `conflict`, `plan_limit_exceeded`. */
  readonly code: string;
  /** Per-field validation messages (422). */
  readonly fields: Record<string, string> | undefined;
  /** Extra structured details (e.g. plan limit info for 402). */
  readonly details: Record<string, unknown> | undefined;
  /** `X-Request-Id` response header, when the server or a proxy sets one. */
  readonly requestId: string | undefined;
  /** Response headers. */
  readonly headers: Headers;
  /** Parsed response body (or raw text if it was not JSON). */
  readonly body: unknown;

  constructor(status: number, error: ErrorBody, headers: Headers, body: unknown) {
    super(`${status} ${error.code}: ${error.message}`);
    this.status = status;
    this.code = error.code;
    this.fields = error.fields;
    this.details = error.details;
    this.headers = headers;
    this.body = body;
    this.requestId = headers.get("x-request-id") ?? undefined;
  }
}

/** 400 — malformed request (e.g. invalid JSON). */
export class BadRequestError extends APIError {}
/** 401 — missing, invalid, expired or revoked API key. */
export class AuthenticationError extends APIError {}
/**
 * 403 — the key lacks a scope, or (code `not_local`) a keyless request reached
 * a local-mode server from somewhere other than this machine.
 */
export class PermissionDeniedError extends APIError {
  /** True when a local-mode server refused a request that does not come from this machine. */
  get isNotLocal(): boolean {
    return this.code === "not_local";
  }
}
/** 404 — the resource does not exist in this workspace. */
export class NotFoundError extends APIError {}
/** 409 — conflict, e.g. the item was already decided or `expected_version` did not match. */
export class ConflictError extends APIError {}
/** 410 — the resource is gone (e.g. an expired device link). */
export class GoneError extends APIError {}
/** 413 — the upload or body is too large. */
export class PayloadTooLargeError extends APIError {}
/** 415 — the uploaded file type is not accepted (e.g. SVG/HTML). */
export class UnsupportedMediaTypeError extends APIError {}

/** 422 — validation failed; see `fields` for per-field messages. */
export class ValidationError extends APIError {}

/** 402 `plan_limit_exceeded` — the workspace plan does not allow this (cloud mode). */
export class PlanLimitError extends APIError {
  /** Which limit was hit, e.g. `webhooks`, `items_per_month`. */
  get limit(): string | undefined {
    return typeof this.details?.limit === "string" ? this.details.limit : undefined;
  }
  /** The plan's maximum for this limit. */
  get max(): number | undefined {
    return typeof this.details?.max === "number" ? this.details.max : undefined;
  }
  /** Current usage. */
  get used(): number | undefined {
    return typeof this.details?.used === "number" ? this.details.used : undefined;
  }
  /** Plan id, e.g. `free`. */
  get plan(): string | undefined {
    return typeof this.details?.plan === "string" ? this.details.plan : undefined;
  }
}

/** 429 — rate limited. `retryAfter` is in seconds when the server sent `Retry-After`. */
export class RateLimitError extends APIError {
  get retryAfter(): number | undefined {
    return parseRetryAfter(this.headers.get("retry-after"));
  }
}

/** 5xx — the server failed. Retried automatically before this is thrown. */
export class InternalServerError extends APIError {}

/** The request never got a response (DNS, refused connection, reset, ...). */
export class APIConnectionError extends StaqlyError {
  constructor(message = "Connection error", options?: { cause?: unknown }) {
    super(message, options);
  }
}

/** The request exceeded the client `timeout`. */
export class TimeoutError extends APIConnectionError {
  constructor(message = "Request timed out", options?: { cause?: unknown }) {
    super(message, options);
  }
}

/** The caller aborted the request through its `AbortSignal`. */
export class APIUserAbortError extends StaqlyError {
  constructor(message = "Request was aborted", options?: { cause?: unknown }) {
    super(message, options);
  }
}

/** `items.waitForDecision` gave up before the item left `pending`. */
export class WaitTimeoutError extends StaqlyError {
  constructor(
    readonly itemId: string,
    /** Seconds waited. */
    readonly waited: number,
    /** The item as last seen (still pending). */
    readonly item: Item | undefined,
  ) {
    super(`Item ${itemId} was not decided within ${waited}s`);
  }
}

/** A reviewer rejected the item (thrown by `requiresApproval`, never by `waitForDecision`). */
export class RejectedError extends StaqlyError {
  constructor(readonly item: Item) {
    super(`Item ${item.id} was ${item.status}${item.feedback ? `: ${item.feedback}` : ""}`);
  }
  get feedback(): string {
    return this.item.feedback;
  }
}

/** A webhook request failed signature or timestamp verification. */
export class WebhookVerificationError extends StaqlyError {}

/** Parses a Retry-After header (seconds or HTTP date) into seconds. */
export function parseRetryAfter(value: string | null | undefined): number | undefined {
  if (!value) return undefined;
  const secs = Number(value);
  if (Number.isFinite(secs)) return Math.max(0, secs);
  const date = Date.parse(value);
  if (Number.isNaN(date)) return undefined;
  return Math.max(0, (date - Date.now()) / 1000);
}

function isErrorBody(v: unknown): v is { error: ErrorBody } {
  if (typeof v !== "object" || v === null || !("error" in v)) return false;
  const e = (v as { error: unknown }).error;
  return typeof e === "object" && e !== null && typeof (e as ErrorBody).code === "string";
}

/** Builds the typed error for a failed response. */
export function makeAPIError(status: number, headers: Headers, body: unknown): APIError {
  const err: ErrorBody = isErrorBody(body)
    ? body.error
    : { code: "http_error", message: typeof body === "string" && body ? body.slice(0, 500) : `HTTP ${status}` };
  if (err.code === "plan_limit_exceeded" || status === 402) return new PlanLimitError(status, err, headers, body);
  switch (status) {
    case 400:
      return new BadRequestError(status, err, headers, body);
    case 401:
      return new AuthenticationError(status, err, headers, body);
    case 403:
      return new PermissionDeniedError(status, err, headers, body);
    case 404:
      return new NotFoundError(status, err, headers, body);
    case 409:
      return new ConflictError(status, err, headers, body);
    case 410:
      return new GoneError(status, err, headers, body);
    case 413:
      return new PayloadTooLargeError(status, err, headers, body);
    case 415:
      return new UnsupportedMediaTypeError(status, err, headers, body);
    case 422:
      return new ValidationError(status, err, headers, body);
    case 429:
      return new RateLimitError(status, err, headers, body);
  }
  if (status >= 500) return new InternalServerError(status, err, headers, body);
  return new APIError(status, err, headers, body);
}
