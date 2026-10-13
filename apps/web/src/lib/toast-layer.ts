/**
 * Lets a notification be used while a dialog is open. A Radix modal (every
 * dialog, sheet and alert in both halves of the app) turns off pointer events
 * on <body> and closes on any press outside itself. The toasts sit above it
 * (z-index), but inherited `pointer-events: none` (toast.css gives it back),
 * and pressing one - "Reload" on the update notice, its close button - would
 * also have dismissed the dialog under it.
 *
 * Radix tells its layers of a press or focus outside with these events, sent
 * to the element pressed; cancelled, the layer stays. They do not bubble, so
 * this listens while they are captured.
 */
const OUTSIDE_EVENTS = ['dismissableLayer.pointerDownOutside', 'dismissableLayer.focusOutside']

export function keepToastsInteractive(root: Pick<Document, 'addEventListener'> = document): void {
  const keep = (event: Event) => {
    const target = event.target as Partial<Pick<Element, 'closest'>> | null
    if (target?.closest?.('[data-sonner-toaster]')) event.preventDefault()
  }
  for (const type of OUTSIDE_EVENTS) root.addEventListener(type, keep, { capture: true })
}
