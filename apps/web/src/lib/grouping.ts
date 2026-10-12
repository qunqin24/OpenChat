import type { Conversation } from '@openchat/api-client'
import type { TFunction } from 'i18next'

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR
const WEEK = 7 * DAY

/** The typed `t` from `useTranslation()`, so keys here are checked too. */
type Translate = TFunction<'common'>

export function parseTimestamp(value?: string): Date | null {
  if (!value) return null
  const normalized = value.includes('T') ? value : value.replace(' ', 'T')
  const parsed = new Date(normalized)
  return Number.isNaN(parsed.getTime()) ? null : parsed
}

/** Compact relative label, matching the `21小时前` / `6天前` / `8周前` style. */
export function formatRelative(value: string | undefined, t: Translate): string {
  const date = parseTimestamp(value)
  if (!date) return ''

  const delta = Date.now() - date.getTime()
  if (delta < MINUTE) return t('chat.time.now')
  if (delta < HOUR) return t('chat.time.minutes', { n: Math.floor(delta / MINUTE) })
  if (delta < DAY) return t('chat.time.hours', { n: Math.floor(delta / HOUR) })
  if (delta < WEEK) return t('chat.time.days', { n: Math.floor(delta / DAY) })
  return t('chat.time.weeks', { n: Math.floor(delta / WEEK) })
}

export interface ConversationGroup {
  key: string
  label: string
  items: Conversation[]
}

function startOfDay(date: Date): number {
  const copy = new Date(date)
  copy.setHours(0, 0, 0, 0)
  return copy.getTime()
}

/**
 * Bucket conversations into 今天 / 昨天 / 过去 7 天 / 过去 30 天 / 月份,
 * preserving the incoming (already recency-sorted) order inside each bucket.
 */
export function groupConversations(history: Conversation[], t: Translate, locale?: string): ConversationGroup[] {
  const today = startOfDay(new Date())
  const groups: ConversationGroup[] = []
  const index = new Map<string, ConversationGroup>()

  const push = (key: string, label: string, item: Conversation) => {
    let group = index.get(key)
    if (!group) {
      group = { key, label, items: [] }
      index.set(key, group)
      groups.push(group)
    }
    group.items.push(item)
  }

  for (const conversation of history) {
    const date = parseTimestamp(conversation.updatedAt)
    if (!date) {
      push('recent', t('chat.group.today'), conversation)
      continue
    }

    const days = Math.round((today - startOfDay(date)) / DAY)
    if (days <= 0) push('recent', t('chat.group.today'), conversation)
    else if (days === 1) push('yesterday', t('chat.group.yesterday'), conversation)
    else if (days <= 7) push('week', t('chat.group.week'), conversation)
    else if (days <= 30) push('month', t('chat.group.month'), conversation)
    else {
      const key = `${date.getFullYear()}-${date.getMonth()}`
      const label = new Intl.DateTimeFormat(locale, {
        year: date.getFullYear() === new Date().getFullYear() ? undefined : 'numeric',
        month: 'long',
      }).format(date)
      push(key, label, conversation)
    }
  }

  return groups
}
