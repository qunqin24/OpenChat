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

/** The splash markup, kept in one place so the preview cannot drift from it. */
const BOOT_MARKUP = `
  <svg viewBox="0 0 32 32" fill="none" aria-hidden="true">
    <path
      d="M16 4C9.4 4 4 8.9 4 15c0 3.2 1.5 6.1 3.9 8.1L6.6 28l5.3-2.5c1.3.3 2.7.5 4.1.5 6.6 0 12-4.9 12-11S22.6 4 16 4Z"
      stroke="currentColor" stroke-width="2.5" stroke-linejoin="round" />
    <path d="M11 13.5h10M11 18h6" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" />
  </svg>
  <div id="boot-bar"></div>
`

/**
 * Re-creates the splash after it has been dismissed, for the preview page.
 *
 * The styles live in index.html's inline <style>, which applies to the whole
 * document — so an element with the same id picks them up without any of the
 * CSS being duplicated here.
 */
export function showBootSplash(): () => void {
  if (document.getElementById('boot')) return () => undefined

  const boot = document.createElement('div')
  boot.id = 'boot'
  boot.setAttribute('role', 'status')
  boot.innerHTML = BOOT_MARKUP
  document.body.appendChild(boot)

  return () => boot.remove()
}
