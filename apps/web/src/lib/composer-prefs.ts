import type { ReasoningEffort } from '@openchat/api-client'

/**
 * The composer's last choices — model, thinking effort, web search — so a new
 * chat starts the way the previous one was set rather than from defaults. A
 * conversation keeps its own settings on the server; these are only what an
 * empty chat opens with.
 *
 * Per account and per browser, in localStorage: a convenience, not state that
 * must survive. Blocked or wiped storage just means the defaults.
 */
export interface ComposerPrefs {
  model?: string
  effort?: ReasoningEffort
  webSearch?: boolean
}

const key = (userId: string) => `openchat.composer.${userId}`

export function loadComposerPrefs(userId: string | undefined): ComposerPrefs {
  if (!userId) return {}
  try {
    const raw = localStorage.getItem(key(userId))
    const parsed: unknown = raw ? JSON.parse(raw) : null
    return parsed && typeof parsed === 'object' ? (parsed as ComposerPrefs) : {}
  } catch {
    return {}
  }
}

export function saveComposerPrefs(userId: string | undefined, change: ComposerPrefs) {
  if (!userId) return
  try {
    localStorage.setItem(key(userId), JSON.stringify({ ...loadComposerPrefs(userId), ...change }))
  } catch {
    // Full or blocked: the next chat opens with the defaults.
  }
}
