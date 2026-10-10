/**
 * An answer the local API gives that is not a success, in the shape the server's
 * errors have (`{ code, message, field? }`), so that the page's error handling -
 * `isOpenChatError`, the codes it has words for - works the same on either side.
 */
export class GuestError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly field?: string,
  ) {
    super(message)
    this.name = 'GuestError'
  }

  toResponse(): Response {
    return Response.json(
      { code: this.code, message: this.message, ...(this.field ? { field: this.field } : {}) },
      { status: this.status },
    )
  }
}

export const notFound = () => new GuestError(404, 'not_found', 'resource not found')
