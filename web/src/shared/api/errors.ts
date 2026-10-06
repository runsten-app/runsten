import type { components } from './schema'

export type ErrorCode = components['schemas']['ErrorDetail']['code']

// ApiError is a failed call to runsten-api: its status, and the stable code of the
// contract's error body ({"error": {"code", "message"}}). A response without that body
// (runsten-web's relay, a proxy) keeps its status, with the code "unavailable" for 502
// to 504 and "internal" otherwise.
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: ErrorCode | 'unavailable' | 'network',
    message: string,
    // Seconds before the next attempt, from Retry-After (too_many_attempts).
    readonly retryAfter?: number,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export function isApiError(e: unknown, code?: ApiError['code']): e is ApiError {
  return e instanceof ApiError && (code === undefined || e.code === code)
}
