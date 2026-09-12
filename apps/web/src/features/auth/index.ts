import { createHttpAuthApi } from './http'

export * from './types'

/** Every caller goes through this, so the transport stays swappable. */
export const authApi = createHttpAuthApi()
