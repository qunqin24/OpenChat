import { describe, expect, it } from 'vitest'

import { defaultSendUserId } from './send-user-id'

describe('defaultSendUserId', () => {
  it('is on for the vendors own endpoints', () => {
    expect(defaultSendUserId('https://api.openai.com/v1')).toBe(true)
    expect(defaultSendUserId('https://API.anthropic.com:443/v1')).toBe(true)
  })

  it('is off for everything else', () => {
    expect(defaultSendUserId('https://relay.example.com/v1')).toBe(false)
    expect(defaultSendUserId('https://api.openai.com.evil.example/v1')).toBe(false)
    expect(defaultSendUserId('https://relay.example.com/api.openai.com')).toBe(false)
    expect(defaultSendUserId('')).toBe(false)
    expect(defaultSendUserId(undefined)).toBe(false)
    expect(defaultSendUserId('not a url')).toBe(false)
  })
})
