import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { COMPACTING, RETRYING, WORKING } from '@/lib/activity-orb'
import { mountOrb, type OrbLook, type OrbOptions } from '@/lib/orb-core'
import { cn } from '@/lib/utils'

/** The size every orb is tuned to read at. */
const SIZE = 20

/**
 * A sphere of dots that moves by what the model is doing (Thinking Orbs,
 * lib/orb-core.ts; which look for what in lib/activity-orb.ts): the mark for
 * "the model is working on it" - before its first word, while it thinks,
 * while a call runs. It keeps moving under reduced motion: a still indicator
 * reads as a stuck one. Off screen it does not draw. `still`, it is drawn
 * once and never moves: the mark of thinking that is over, in its line's colour.
 */
export function ThinkingOrb({ look = WORKING, still = false }: { look?: OrbLook; still?: boolean }) {
  const ref = useRef<SVGSVGElement>(null)
  const { state, variant } = look
  useEffect(() => {
    // Rebuilt together: a variant belongs to its state, which TypeScript loses once they are apart.
    const orb = mountOrb(ref.current!, { state, variant, size: SIZE } as OrbOptions)
    if (still) orb.pause()
    return orb.destroy
  }, [state, variant, still])
  return (
    <svg
      ref={ref}
      className={cn('n-orb', still && 'is-still')}
      width={SIZE}
      height={SIZE}
      viewBox={`0 0 ${SIZE} ${SIZE}`}
      fill="currentColor"
      aria-hidden="true"
      data-motion="essential"
    />
  )
}

/**
 * The run is compressing the conversation's context before its next request:
 * the orb and what is happening, in the place the thinking line takes, since
 * nothing else is moving while it does.
 */
export function CompactingLine() {
  const { t } = useTranslation()
  return (
    <div className="n-thinking" role="status">
      <ThinkingOrb look={COMPACTING} />
      <span className="n-reasoning-live" data-motion="essential">
        {t('chat.compaction.running')}
      </span>
    </div>
  )
}

/**
 * A reply with nothing in it yet: the orb and "Thinking…", worn the same way
 * as the reasoning toggle, so the line carries on into it unchanged when the
 * model starts to think.
 */
/** The model at work, or a failed request being tried again. */
export function ThinkingLine({ retrying = false }: { retrying?: boolean }) {
  const { t } = useTranslation()
  return (
    <div className="n-thinking" role="status">
      <ThinkingOrb look={retrying ? RETRYING : WORKING} />
      <span className="n-reasoning-live" data-motion="essential">
        {t(retrying ? 'chat.retrying' : 'chat.reasoning.live')}
      </span>
    </div>
  )
}
