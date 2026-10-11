import { describe, expect, it } from 'vitest'

import { adopt, followAddress, openView, release, type ChatView } from './chat-view'

describe('followAddress', () => {
  it('keeps the page when the address has not changed', () => {
    const view = openView('a')
    expect(followAddress(view, 'a')).toBe(view)
  })

  it('starts the page over when another conversation is opened', () => {
    expect(followAddress(openView('a'), 'b')).toEqual({ epoch: 1, route: 'b', shown: 'b' })
  })

  it('starts over when the new-chat screen is opened from a conversation, and the other way round', () => {
    const toNew = followAddress(openView('a'), undefined)
    expect(toNew).toEqual({ epoch: 1, route: undefined, shown: undefined })
    expect(followAddress(toNew, 'a')).toEqual({ epoch: 2, route: 'a', shown: 'a' })
  })

  it('keeps the page when the address moves to the conversation the page made itself', () => {
    const made = adopt(openView(undefined), 'x')
    expect(made).toEqual({ epoch: 0, route: undefined, shown: 'x' })
    const followed = followAddress(made, 'x')
    expect(followed).toEqual({ epoch: 0, route: 'x', shown: 'x' })
    // And the page is an ordinary one from there on: another conversation resets it.
    expect(followAddress(followed, 'y').epoch).toBe(1)
  })

  it('starts over when the address goes somewhere else while the page is ahead of it', () => {
    const made = adopt(openView(undefined), 'x')
    expect(followAddress(made, 'y')).toEqual({ epoch: 1, route: 'y', shown: 'y' })
  })

  it('starts over when the address leaves the conversation the page made', () => {
    const followed = followAddress(adopt(openView(undefined), 'x'), 'x')
    expect(followAddress(followed, undefined).epoch).toBe(1)
  })

  it('keeps the page when the address reaches the empty screen after the page gave a conversation up', () => {
    const view: ChatView = { epoch: 0, route: 'x', shown: 'x' }
    const released = release(view, 'x')
    expect(released.shown).toBeUndefined()
    expect(followAddress(released, undefined)).toEqual({ epoch: 0, route: undefined, shown: undefined })
  })
})

describe('adopt', () => {
  it('is for the empty page only', () => {
    const view = openView('a')
    expect(adopt(view, 'x')).toBe(view)
  })
})

describe('release', () => {
  it('lets go of the conversation asked about and no other', () => {
    const view = adopt(openView(undefined), 'x')
    expect(release(view, 'other')).toBe(view)
    expect(release(view, 'x').shown).toBeUndefined()
  })
})
