import type { BrowserProgress } from '@openchat/api-client'
import type { TFunction } from 'i18next'

import { siteName } from './urls'

// The browser tool in a reply (docs/design/browser-plan.md): each call is one
// action on the conversation's browser, reported as it runs (`BrowserProgress`)
// and left in its result's details. An action that changes something on a
// site waits for the person: `waiting`, with the id to answer.

export const BROWSER_TOOL = 'browser'

function isProgress(value: unknown): value is BrowserProgress {
  return typeof value === 'object' && value !== null && typeof (value as BrowserProgress).action === 'string' && typeof (value as BrowserProgress).status === 'string'
}

/** What a browser step reports: its result's details once done, else its live progress. */
export function browserProgressOf(details: unknown, progress: unknown): BrowserProgress | null {
  if (isProgress(details)) return details
  if (isProgress(progress)) return progress
  return null
}

/** The element's name as the snapshot line has it - `button "Send"` → Send - else the whole line. */
export function targetName(target: string | undefined): string {
  if (!target) return ''
  const quoted = /"((?:\\.|[^"\\])*)"/.exec(target)
  return quoted ? quoted[1]!.replace(/\\(.)/g, '$1') : target
}

type T = TFunction<'common'>

/** The step's line: what the browser did, or is doing. */
export function browserStepLabel(t: T, args: Record<string, unknown>, progress: BrowserProgress | null, running: boolean): string {
  const action = progress?.action ?? (typeof args.action === 'string' ? args.action : '')
  const str = (value: unknown) => (typeof value === 'string' ? value : '')
  const target = targetName(progress?.target) || str(args.ref)
  const url = progress?.url ?? str(args.url)
  const values = {
    host: url ? siteName(url) : '',
    target,
    text: str(args.text),
    key: str(args.key),
    values: Array.isArray(args.values) ? args.values.filter((v) => typeof v === 'string').join(t('browser.step.joiner')) : '',
  }
  if (progress?.status === 'waiting') return t('browser.step.waiting', { host: progress.site ?? values.host })
  const known = ['open', 'snapshot', 'find', 'click', 'type', 'select', 'press', 'scroll', 'back', 'wait', 'screenshot', 'read']
  if (!known.includes(action)) return running ? t('browser.step.preparing') : t('browser.step.generic')
  let key = action
  if (action === 'scroll') key = args.direction === 'up' ? 'scroll-up' : 'scroll-down'
  if (action === 'wait' && !values.text) key = 'wait-time'
  if (action === 'type' && !values.text) key = 'type-empty'
  if (action === 'open' && !values.host) return t('browser.step.preparing')
  return t(`browser.step.${running ? 'doing' : 'done'}.${key}` as 'browser.step.done.open', values)
}
