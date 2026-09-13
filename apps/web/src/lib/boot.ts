/**
 * Removes the splash markup that index.html paints before any JavaScript runs.
 *
 * Called once the router reports itself idle rather than on mount: the root
 * layout renders while the first route is still resolving its session check,
 * so dismissing on mount would swap the splash for an empty shell.
 */
export function dismissBootSplash(): void {
  const boot = document.getElementById('boot')
  if (!boot || boot.dataset.leaving === 'true') return

  boot.dataset.leaving = 'true'
  boot.addEventListener('transitionend', () => boot.remove(), { once: true })

  // transitionend never fires under reduced motion, or in a background tab
  // where transitions are skipped, so the element would linger invisibly over
  // the app and swallow every click.
  window.setTimeout(() => boot.remove(), 600)
}

/**
 * The splash markup, read from the live element rather than restated here.
 *
 * This module is imported during bootstrap, while index.html's `#boot` is still
 * on the page, so the capture always succeeds in the app. Holding a second copy
 * in a template literal is what let the preview go on showing the old logo
 * after the real splash had been redesigned — there is now one copy in the
 * codebase, and it is the one that actually paints.
 */
const BOOT_MARKUP = document.getElementById('boot')?.innerHTML ?? ''

/**
 * Re-creates the splash after it has been dismissed, for the preview page.
 *
 * The styles live in index.html's inline <style>, which applies to the whole
 * document — so an element with the same id picks them up without any of the
 * CSS being duplicated here.
 */
export function showBootSplash(): () => void {
  if (document.getElementById('boot')) return () => undefined
  // Empty only if this module was first imported after the splash had gone,
  // which bootstrap order rules out; showing nothing beats showing a stale
  // hand-written copy.
  if (!BOOT_MARKUP) return () => undefined

  const boot = document.createElement('div')
  boot.id = 'boot'
  boot.setAttribute('role', 'status')
  boot.innerHTML = BOOT_MARKUP
  document.body.appendChild(boot)

  return () => boot.remove()
}
