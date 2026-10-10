import type { ApiError } from './types'

/** Thrown for any non-2xx response, carrying the server's error object. */
export class OpenChatError extends Error {
  readonly status: number
  readonly code: string
  /** Set when one input caused the failure, so a form can blame that field. */
  readonly field?: string
  readonly details?: Record<string, unknown>
  /** The server's id for the request (X-Request-Id): what to quote in a bug
   *  report, and what finds the request's lines in the server log. */
  readonly requestId?: string

  constructor(status: number, body: ApiError, requestId?: string) {
    super(body.message)
    this.name = 'OpenChatError'
    this.status = status
    this.code = body.code
    this.field = body.field
    this.details = body.details
    this.requestId = requestId || undefined
  }

  static async fromResponse(response: Response): Promise<OpenChatError> {
    let body: ApiError = { code: 'unknown_error', message: response.statusText }
    try {
      body = (await response.json()) as ApiError
    } catch {
      // Non-JSON error body (proxy, gateway); keep the status-text fallback.
    }
    return new OpenChatError(response.status, body, response.headers.get('X-Request-Id') ?? undefined)
  }
}

export function isOpenChatError(error: unknown): error is OpenChatError {
  return error instanceof OpenChatError
}
