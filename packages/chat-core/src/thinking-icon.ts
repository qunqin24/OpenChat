/**
 * The thinking-effort mark - a bulb - as drawing data, so the web and the app
 * (each a components/chat/ThinkingIcon.tsx of its own) draw the same one. A
 * 24-unit square, stroked and unfilled with round ends, as lucide's icons are,
 * so it sits among them. No rays: they made it busy at the size it is drawn;
 * without them the bulb fills the square's height, as lucide's own bulb does,
 * centred in it.
 */
export const THINKING_ICON_PATHS = [
  // The bulb, its neck narrowing into the base.
  'M12 2.25a6.5 6.5 0 0 0-4.03 11.57c.78.65 1.17 1.43 1.17 2.34v.39h5.72v-.39c0-.91.39-1.69 1.17-2.34A6.5 6.5 0 0 0 12 2.25Z',
  // The base, in two bands.
  'M8.36 17.85h7.28',
  'M9.66 21.75h4.68',
] as const
