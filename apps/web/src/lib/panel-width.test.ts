import { describe, expect, it } from 'vitest'

import { CHAT_MIN, PANEL_MIN, clampPanelWidth } from './panel-width'

describe('clampPanelWidth', () => {
  it('keeps the panel wide enough and leaves the chat its room', () => {
    expect(clampPanelWidth(600, 1600)).toBe(600)
    expect(clampPanelWidth(100, 1600)).toBe(PANEL_MIN)
    expect(clampPanelWidth(1500, 1600)).toBe(1600 - CHAT_MIN)
    expect(clampPanelWidth(500.4, 1600)).toBe(500)
  })
})
