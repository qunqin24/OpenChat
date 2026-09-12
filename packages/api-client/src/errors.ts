import type { ApiError } from './types'

/** Thrown for any non-2xx response, carrying the server's error object. */
export class OpenChatError extends Error {
  readonly status: number
  readonly code: string
  readonly details?: Record<string, unknown>

  constructor(status: number, body: ApiError) {
    super(body.message)
    this.name = 'OpenChatError'
    this.status = status
    this.code = body.code
    this.details = body.details
  }

  static async fromResponse(response: Response): Promise<OpenChatError> {
    let body: ApiError = { code: 'unknown_error', message: response.statusText }
    try {
      body = (await response.json()) as ApiError
    } catch {
      // Non-JSON error body (proxy, gateway); keep the status-text fallback.
    }
    return new OpenChatError(response.status, body)
  }
}

export function isOpenChatError(error: unknown): error is OpenChatError {
  return error instanceof OpenChatError
}
