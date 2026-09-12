import { cn } from '@/lib/utils'

interface ModelAvatarProps {
  /** Model id or display name; drives both the initial and the tint. */
  name: string
  size?: number
  className?: string
}

/**
 * Placeholder avatar for a model.
 *
 * The prism original maps each vendor to its brand mark. Until the backend
 * serves a provider catalogue there is nothing to look a mark up by, so this
 * draws a neutral monogram instead — deliberately zero-chroma, matching the
 * surface greys rather than introducing a hue the palette reserves for status.
 */
export function ModelAvatar({ name, size = 32, className }: ModelAvatarProps) {
  const initial = (name.trim()[0] ?? '?').toUpperCase()

  return (
    <span
      aria-hidden="true"
      className={cn('inline-flex shrink-0 items-center justify-center rounded-full', className)}
      style={{
        width: size,
        height: size,
        background: 'var(--n-panel)',
        boxShadow: 'var(--n-ring)',
        color: 'var(--n-text-2)',
        fontSize: Math.round(size * 0.42),
        fontWeight: 600,
        lineHeight: 1,
      }}
    >
      {initial}
    </span>
  )
}

export default ModelAvatar
