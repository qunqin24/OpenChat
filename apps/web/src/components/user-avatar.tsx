import { cn } from '@/lib/utils'

/**
 * An account's picture: the first letter of its name, white on a colour of its
 * own — the way Google draws accounts without a photo.
 *
 * The colour comes from the account's id, so it is the same on every page and
 * every device, survives a change of name, and two people called Alex still
 * tell apart. The palette is saturated mid-tones that carry white text in both
 * themes, so the avatar needs nothing from either design system: the chat and
 * the admin panel draw the same one, sized and shaped by the caller.
 *
 * Nothing is fetched. Pictures used to come from Gravatar, which told it who
 * was being looked at from every page that showed a name.
 */
export function UserAvatar({
  id,
  name,
  email,
  className,
}: {
  /** Seeds the colour. */
  id?: string
  name?: string
  email?: string
  className?: string
}) {
  const letter = initialOf(name, email)
  return (
    <span
      aria-hidden="true"
      className={cn('inline-flex shrink-0 select-none items-center justify-center rounded-full font-medium text-white', className)}
      style={{ backgroundColor: colorOf(id || email || name || '') }}
    >
      {letter}
    </span>
  )
}

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
function colorOf(seed: string): string {
  let hash = 0x811c9dc5
  for (let i = 0; i < seed.length; i++) {
    hash ^= seed.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }
  return PALETTE[(hash >>> 0) % PALETTE.length]!
}

/**
 * The first letter of the name — a whole character, so 张 stays 张 and an
 * emoji is not cut in half — or of the address's local part when there is no
 * name.
 */
function initialOf(name?: string, email?: string): string {
  const source = (name ?? '').trim() || (email ?? '').split('@')[0]?.trim() || ''
  const first = [...source][0]
  return first ? first.toLocaleUpperCase() : '?'
}

export default UserAvatar
