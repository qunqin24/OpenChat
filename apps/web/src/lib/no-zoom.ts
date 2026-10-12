/**
 * Keeps the page at its own scale on iOS. Safari ignores `user-scalable=no`
 * and `maximum-scale` for pinching (since iOS 10) and does not honour
 * `touch-action: pan-x pan-y` for it everywhere either; cancelling its gesture
 * events is what stops the pinch. Other browsers never fire them, and follow the
 * viewport meta and touch-action instead (index.html, globals.css).
 */
export function preventZoom(target: Pick<Document, 'addEventListener'> = document): void {
  const cancel = (event: Event) => event.preventDefault()
  for (const type of ['gesturestart', 'gesturechange', 'gestureend']) {
    target.addEventListener(type, cancel, { passive: false })
  }
}
