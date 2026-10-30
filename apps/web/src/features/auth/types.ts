import type { SessionUser } from '@openchat/chat-core/auth'

// The shared inputs, outcomes and errors of signing in are chat-core's (auth).
export {
  AuthFieldError,
  isAuthFieldError,
  type AdminContact,
  type AuthErrorKey,
  type LoginInput,
  type RegisterInput,
  type RegisterOutcome,
  type SessionUser,
} from '@openchat/chat-core/auth'

/** Only the fields present are changed. */
export interface UpdateProfileInput {
  name?: string
  email?: string
}

export interface ChangePasswordInput {
  currentPassword: string
  newPassword: string
}

/**
 * Every auth call goes through this interface.
 *
 * Shaped for HttpOnly session cookies: nothing here returns or accepts a token,
 * because the browser carries the cookie and JavaScript never sees it. "Am I
 * signed in?" is answered by asking the server, not by reading local state.
 */
import type { LoginInput, RegisterInput, RegisterOutcome } from '@openchat/chat-core/auth'

export interface AuthApi {
  /** Resolves to null when there is no valid session. */
  session(): Promise<SessionUser | null>
  login(input: LoginInput): Promise<SessionUser>
  register(input: RegisterInput): Promise<RegisterOutcome>
  logout(): Promise<void>
  /** Self-service: cannot reach role or enabled state, by contract. */
  updateProfile(input: UpdateProfileInput): Promise<SessionUser>
  /** Requires the current password; other sessions are revoked, this one is not. */
  changePassword(input: ChangePasswordInput): Promise<void>
  /**
   * Signs in with a passkey the browser offers: the server's challenge, the
   * browser's prompt, the answer. Resolves to null when the person closed the
   * prompt, which is no failure.
   */
  passkeyLogin(): Promise<SessionUser | null>
}
