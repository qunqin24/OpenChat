/**
 * How a script-driven scroll should move: animated, unless the reader has asked
 * their system for less motion. CSS cannot see these - `scroll-behavior` in a
 * stylesheet is honoured by the media query there, but `scrollTo({ behavior:
 * 'smooth' })` is an explicit request that ignores it.
 */
export function smoothScrollBehavior(): ScrollBehavior {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return 'auto'
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'
}
