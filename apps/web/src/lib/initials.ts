/** Up to two initials for an avatar fallback, from a name or an email. */
export function initialsOf(value?: string): string {
  const source = (value ?? '').trim()
  if (!source) return '?'

  // An address has no word boundaries worth reading; take its local part.
  const label = source.includes('@') ? source.split('@')[0] ?? source : source
  const words = label.split(/[\s._-]+/).filter(Boolean)

  const letters = words.length > 1 ? [words[0]?.[0], words[1]?.[0]] : [label[0], label[1]]
  return letters.filter(Boolean).join('').toUpperCase()
}
