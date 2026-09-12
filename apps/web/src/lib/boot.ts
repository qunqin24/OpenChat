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
