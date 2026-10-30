import { describe, expect, it } from 'vitest'

import { newChatState } from './chat-view'

describe('newChatState', () => {
  it('is a different mark every time, keeping the rest of the state', () => {
    const a = newChatState({ other: 1 } as never)
    const b = newChatState({ other: 1 } as never)
    expect(a.newChat).not.toBe(b.newChat)
    expect((a as { other?: number }).other).toBe(1)
  })
})
