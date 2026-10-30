// Which greeting lines there are and how one is picked are chat-core's
// (greeting); the last one shown is remembered per browser here.

/**
 * The last line shown, per browser: a convenience, not state that must survive
 * - without it the next line may repeat the last.
 */
const KEY = 'openchat.last-greeting'

export function loadLastGreeting(): string | null {
  try {
    return localStorage.getItem(KEY)
  } catch {
    return null
  }
}

export function saveLastGreeting(key: string) {
  try {
    localStorage.setItem(KEY, key)
  } catch {
    // Private mode or blocked storage: a line may repeat.
  }
}
