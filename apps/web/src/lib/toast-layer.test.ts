import { describe, expect, it } from 'vitest'

import { keepToastsInteractive } from './toast-layer'

/** An element-like target: `inToast` says whether it sits in the toaster. */
function element(inToast: boolean) {
  const target = new EventTarget() as EventTarget & { closest: (selector: string) => unknown }
  target.closest = (selector) => (inToast && selector === '[data-sonner-toaster]' ? {} : null)
  keepToastsInteractive(target as unknown as Document)
  return target
}

const pressOutside = (target: EventTarget, type = 'dismissableLayer.pointerDownOutside') => {
  const event = new CustomEvent(type, { bubbles: false, cancelable: true })
  target.dispatchEvent(event)
  return event.defaultPrevented
}

describe('keepToastsInteractive', () => {
  it('keeps a dialog open when the press or focus outside it was on a toast', () => {
    expect(pressOutside(element(true))).toBe(true)
    expect(pressOutside(element(true), 'dismissableLayer.focusOutside')).toBe(true)
  })

  it('leaves any other press outside to close the dialog', () => {
    expect(pressOutside(element(false))).toBe(false)
  })
})
