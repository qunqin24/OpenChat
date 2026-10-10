import { useTranslation } from 'react-i18next'

/**
 * Nine dots in a square, lighting up in a wave from one corner to the other:
 * the mark for "the model is working on it" — before its first word, and
 * while it thinks. Opacity only, and every dot marked essential, so it keeps
 * moving under reduced motion: a still indicator reads as a stuck one.
 */
export function ThinkingOrb() {
  return (
    <span className="n-orb" aria-hidden="true">
      {Array.from({ length: 9 }, (_, i) => (
        <span
          key={i}
          className="n-orb-dot"
          data-motion="essential"
          // The wave runs along the anti-diagonals: row + column.
          style={{ animationDelay: `${(Math.floor(i / 3) + (i % 3)) * 110}ms` }}
        />
      ))}
    </span>
  )
}

/**
 * A reply with nothing in it yet: the orb and "Thinking…", worn the same way
 * as the reasoning toggle, so the line carries on into it unchanged when the
 * model starts to think.
 */
export function ThinkingLine() {
  const { t } = useTranslation()
  return (
    <div className="n-thinking" role="status">
      <ThinkingOrb />
      <span className="n-reasoning-live" data-motion="essential">
        {t('chat.reasoning.live')}
      </span>
    </div>
  )
}
