/**
 * An account's picture when it has none uploaded: the first letter of its
 * name, white on a colour of its own - the way Google draws accounts without a
 * photo. The web (components/user-avatar.tsx) and the app draw the same one.
 *
 * The colour comes from the account's id, so it is the same on every page and
 * every device, survives a change of name, and two people called Alex still
 * tell apart.
 */

/**
 * Google's account colours, in spirit: one hue per account, none too light for
 * white text or too dark to tell from a dark page.
 */
const PALETTE = [
  '#1E88E5', // blue
  '#E53935', // red
  '#43A047', // green
  '#FB8C00', // orange
  '#8E24AA', // purple
  '#00ACC1', // cyan
  '#F4511E', // deep orange
  '#3949AB', // indigo
  '#D81B60', // pink
  '#00897B', // teal
  '#7CB342', // light green
  '#5E35B1', // deep purple
  '#6D4C41', // brown
  '#546E7A', // blue grey
]

/** FNV-1a: a small, well-spread hash, so similar ids land on different colours. */
export function avatarColor(seed: string): string {
  let hash = 0x811c9dc5
  for (let i = 0; i < seed.length; i++) {
    hash ^= seed.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }
  return PALETTE[(hash >>> 0) % PALETTE.length]!
}

/**
 * The first letter of the name - a whole character, so 张 stays 张 and an
 * emoji is not cut in half - or of the address's local part when there is no
 * name.
 */
export function avatarInitial(name?: string, email?: string): string {
  const source = (name ?? '').trim() || (email ?? '').split('@')[0]?.trim() || ''
  const first = [...source][0]
  return first ? first.toLocaleUpperCase() : '?'
}
