import { describe, expect, it } from 'vitest'

import { preventZoom } from './no-zoom'

describe('preventZoom', () => {
  it('cancels the pinch gestures Safari fires', () => {
    const target = new EventTarget()
    preventZoom(target as unknown as Document)
    for (const type of ['gesturestart', 'gesturechange', 'gestureend']) {
      const event = new Event(type, { cancelable: true })
      target.dispatchEvent(event)
      expect(event.defaultPrevented).toBe(true)
    }
  })
})
