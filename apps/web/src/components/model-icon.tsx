import { useEffect, useState } from 'react'

import { cn } from '@/lib/utils'
import { isInkIcon, isMonochrome, resolveIcon } from '@openchat/chat-core/model-icons'

/**
 * A model's or vendor's brand mark (which one: chat-core's `model-icons.ts`,
 * shared with the app), served from our own origin.
 */

const iconUrl = (icon: string, theme?: 'dark') => `/model-icons/${icon}${theme ? '.dark' : ''}.svg`

/** Marks already asked for, held so the decoded images stay in memory. */
const preloaded = new Map<string, HTMLImageElement>()

/**
 * Loads the marks of these models ahead of time, drawing nothing - the model
 * picker holds one for its list - so its menu shows them at once when it
 * opens, rather than fetching and decoding each as it appears.
 */
export function ModelIconPreload({ models }: { models: { providerId?: string | null; family?: string | null }[] }) {
  useEffect(() => {
    for (const model of models) {
      const icon = resolveIcon(undefined, model.providerId || undefined, model.family || undefined, 'model')
      if (!icon || preloaded.has(icon)) continue
      const image = new Image()
      image.src = iconUrl(icon)
      preloaded.set(icon, image)
      if (isInkIcon(icon)) {
        const dark = new Image()
        dark.src = iconUrl(icon, 'dark')
        preloaded.set(`${icon}.dark`, dark)
      }
    }
  }, [models])
  return null
}

type ModelIconProps = {
  /**
   * A vendor brand, as `ChannelTypeInfo.iconId` spells it. Wins over the two
   * below: a caller supplying one has been told which mark to draw.
   */
  brand?: string
  /** models.dev provider id, e.g. `anthropic`, `amazon-bedrock`. */
  providerId?: string
  /** models.dev model family; a sharper mark than a reseller's own. */
  family?: string
  /** Falls back to its first character when no mark is known. */
  name?: string
  /**
   * What the mark stands for. `vendor` (the default) prefers the provider's
   * mark; `model` prefers the model family's — an avatar for Claude Sonnet is
   * Claude's mark even when Anthropic, or a reseller, serves it.
   */
  subject?: 'vendor' | 'model'
  className?: string
}

/**
 * A brand mark, or a neutral square with the first letter in it. Most of the
 * catalogue will land on the letter, so it is a designed state rather than an
 * apology: a broken-image glyph must never appear.
 *
 * The styling here leans on neither design system's tokens — it is plain
 * black/white alpha — so the component stays usable from the chat side too.
 */
export function ModelIcon({ brand, providerId, family, name, subject, className }: ModelIconProps) {
  const icon = resolveIcon(brand, providerId, family, subject)
  // A file can go missing if the map moves ahead of the last icon sync; the
  // letter is a better answer than a broken image. Remembering which name
  // failed rather than a bare flag means a different model reusing this
  // component gets its own attempt, with no effect to reset anything.
  const [failedIcon, setFailedIcon] = useState<string>()

  // The letter sits on a tile, so it is rounded; a mark is drawn as it is, on
  // nothing, so nothing of an odd-shaped one is cut off (object-contain keeps
  // its own proportions inside the box).
  const box = cn('inline-flex size-8 shrink-0 items-center justify-center', className)

  if (!icon || icon === failedIcon) {
    const letter = (name ?? providerId ?? brand ?? '').trim().charAt(0).toUpperCase()
    return (
      <span
        className={cn(
          box,
          'overflow-hidden rounded-md bg-black/5 text-xs font-semibold text-black/55 dark:bg-white/10 dark:text-white/65',
        )}
        aria-hidden="true"
      >
        {letter || '?'}
      </span>
    )
  }

  const image = (src: string, className?: string) => (
    <img
      src={src}
      alt=""
      aria-hidden="true"
      // Not lazy: a mark is a few hundred bytes, and a lazy image is fetched -
      // even from cache - only once its box is laid out and on screen, so
      // every time a menu of models opened, its marks came in a beat late.
      width={32}
      height={32}
      onError={() => setFailedIcon(icon)}
      // No inset. There is no background behind the mark to hold it off, and a
      // fixed one ate half of a `size-4` box — which is how the marks in the
      // channel picker came out visibly smaller than the letters beside them.
      // The mark now fills whatever box the caller asked for, which is also
      // what the letter below does, so the two states are the same size.
      className={cn(box, 'object-contain', className, 'rounded-none')}
    />
  )

  // An ink mark comes in a file per theme; the one the app's theme wants shows.
  if (isInkIcon(icon)) {
    return (
      <>
        {image(iconUrl(icon), 'dark:hidden')}
        {image(iconUrl(icon, 'dark'), 'hidden dark:inline-flex')}
      </>
    )
  }
  return image(iconUrl(icon), isMonochrome(icon) ? 'dark:invert' : undefined)
}
