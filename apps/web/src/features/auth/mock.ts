import { AuthFieldError, type AuthApi, type SessionUser } from './types'

/**
 * In-memory auth, standing in for the backend until the real endpoints exist.
 *
 * The session marker lives in sessionStorage purely so a page refresh does not
 * sign you out. The real implementation stores nothing on the client at all —
 * the cookie is HttpOnly — so this whole module disappears when the backend
 * lands, rather than being ported.
 */

interface MockAccount extends SessionUser {
  password: string
}

const ACCOUNTS_KEY = 'openchat.mock.accounts'
const SESSION_KEY = 'openchat.mock.session'

/** Pre-seeded so the login page is usable without registering first. */
const DEMO: MockAccount = {
  id: 'demo-user',
  email: 'demo@openchat.dev',
  name: 'Demo',
  password: 'password',
}

function read<T>(storage: Storage, key: string, fallback: T): T {
  try {
    const raw = storage.getItem(key)
    return raw ? (JSON.parse(raw) as T) : fallback
  } catch {
    return fallback
  }
}

function write(storage: Storage, key: string, value: unknown): void {
  try {
    storage.setItem(key, JSON.stringify(value))
  } catch {
    // Private mode or blocked storage: the mock just forgets between reloads.
  }
}

function loadAccounts(): MockAccount[] {
  const stored = read<MockAccount[]>(localStorage, ACCOUNTS_KEY, [])
  return stored.some((account) => account.email === DEMO.email) ? stored : [DEMO, ...stored]
}

/** Latency, so loading states are actually visible while building the UI. */
function delay(ms = 450): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}

function toSessionUser({ id, email, name }: MockAccount): SessionUser {
  return { id, email, name }
}

export function createMockAuthApi(): AuthApi {
  return {
    async session() {
      await delay(150)
      const id = read<string | null>(sessionStorage, SESSION_KEY, null)
      if (!id) return null

      const account = loadAccounts().find((entry) => entry.id === id)
      return account ? toSessionUser(account) : null
    },

    async login({ email, password }) {
      await delay()
      const account = loadAccounts().find(
        (entry) => entry.email.toLowerCase() === email.trim().toLowerCase(),
      )

      // Deliberately the same error for an unknown address and a wrong
      // password: telling them apart would let anyone probe for registered
      // addresses.
      if (!account || account.password !== password) {
        throw new AuthFieldError('password', 'auth.error.invalid-credentials')
      }

      write(sessionStorage, SESSION_KEY, account.id)
      return toSessionUser(account)
    },

    async register({ email, name, password }) {
      await delay()
      const accounts = loadAccounts()
      const normalized = email.trim().toLowerCase()

      if (accounts.some((entry) => entry.email.toLowerCase() === normalized)) {
        throw new AuthFieldError('email', 'auth.error.email-taken')
      }

      const account: MockAccount = {
        id: crypto.randomUUID(),
        email: email.trim(),
        name: name.trim(),
        password,
      }

      write(localStorage, ACCOUNTS_KEY, [...accounts, account])
      write(sessionStorage, SESSION_KEY, account.id)
      return toSessionUser(account)
    },

    async logout() {
      await delay(150)
      sessionStorage.removeItem(SESSION_KEY)
    },
  }
}

/** Shown as a hint on the login page while the mock is in use. */
export const DEMO_CREDENTIALS = { email: DEMO.email, password: DEMO.password }
