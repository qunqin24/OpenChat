import { describe, expect, it } from 'vitest'

import { choicesState } from './choices'

const messages = ['Tell me about A.', 'Tell me about B.']

describe('choicesState', () => {
  it('offers the button on the last answer with nothing sent after it', () => {
    expect(choicesState(messages, { complete: true, canPick: true })).toEqual({ taken: null, open: true, live: true })
  })

  it('shows the button disabled while the page cannot send', () => {
    expect(choicesState(messages, { complete: true, canPick: false })).toEqual({ taken: null, open: true, live: false })
  })

  it('waits for a block still being written', () => {
    expect(choicesState(messages, { complete: false, canPick: true })).toEqual({ taken: null, open: false, live: false })
  })

  it('marks the choice that was sent after the reply', () => {
    expect(choicesState(messages, { complete: true, next: ' Tell me about B.\n', canPick: false })).toEqual({
      taken: 1,
      open: false,
      live: false,
    })
  })

  it('settles with nothing marked when something else was written', () => {
    expect(choicesState(messages, { complete: true, next: 'Something else', canPick: false })).toEqual({
      taken: null,
      open: false,
      live: false,
    })
  })
})
