import { createMockAuthApi } from './mock'

export * from './types'
export { DEMO_CREDENTIALS } from './mock'

/**
 * The single switch between the mock and the real backend.
 *
 * Swap `createMockAuthApi()` for `createHttpAuthApi()` (from ./http) once the
 * server serves /auth/session, /auth/login, /auth/register and /auth/logout.
 * Nothing else in the app needs to change: every caller goes through `authApi`.
 */
export const authApi = createMockAuthApi()
