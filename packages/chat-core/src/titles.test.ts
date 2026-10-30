import type { TFunction } from 'i18next'
import { describe, expect, it } from 'vitest'

import { DEFAULT_TITLE, displayTitle } from './titles'

// A stand-in for the translator that answers the one key the helper uses.
const t = ((key: string) => (key === 'chat.untitled' ? '新对话' : key)) as unknown as TFunction

describe('displayTitle', () => {
  it('shows the translated name for the title a conversation has until it is titled', () => {
    expect(displayTitle(DEFAULT_TITLE, t)).toBe('新对话')
  })

  it('shows the translated name for no title at all', () => {
    expect(displayTitle('', t)).toBe('新对话')
    expect(displayTitle('   ', t)).toBe('新对话')
    expect(displayTitle(undefined, t)).toBe('新对话')
    expect(displayTitle(null, t)).toBe('新对话')
  })

  it('leaves a real title as it is', () => {
    expect(displayTitle('Trip to Lisbon', t)).toBe('Trip to Lisbon')
    // Only the exact default: a title that merely contains it is a title.
    expect(displayTitle('New chat about jazz', t)).toBe('New chat about jazz')
  })
})
