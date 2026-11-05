import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { COMPACTING, RETRYING, WORKING } from '@/lib/activity-orb'
import { mountOrb, type OrbLook, type OrbOptions } from '@/lib/orb-core'
import { cn } from '@/lib/utils'

/** The size every orb is tuned to read at. */
const SIZE = 20
/** How long one look gives way to the next (agent.css, n-orb-in / n-orb-out). */
const HANDOVER = 260

/**
 * A sphere of dots that moves by what the model is doing (Thinking Orbs,
 * lib/orb-core.ts; which look for what in lib/activity-orb.ts): the mark for
 * "the model is working on it" - before its first word, while it thinks,
 * while a call runs. It keeps moving under reduced motion: a still indicator
 * reads as a stuck one. Off screen it does not draw. `still`, it is drawn
 * once and never moves: the mark of thinking that is over, in its line's colour.
 *
 * It does not cut in: it grows in as it appears, and a new look grows in over
 * the old one as that fades and shrinks a little (upstream draws one look per
 * orb and has no way from one to another). Under reduced motion both are cuts.
 */
export function ThinkingOrb({ look = WORKING, still = false }: { look?: OrbLook; still?: boolean }) {
  const key = `${look.state}:${look.variant ?? ''}:${still ? 'still' : ''}`
  // The look shown, and the one giving way to it while it fades.
  const [shown, setShown] = useState({ key, look, still })
  const [leaving, setLeaving] = useState<{ key: string; look: OrbLook; still: boolean } | null>(null)
  if (shown.key !== key) {
    setLeaving(shown)
    setShown({ key, look, still })
  }
  useEffect(() => {
    if (!leaving) return
    const timer = setTimeout(() => setLeaving(null), HANDOVER)
    return () => clearTimeout(timer)
  }, [leaving])

  return (
    <span className="n-orb-stack" data-motion="essential">
      {leaving && <OrbLayer key={leaving.key} look={leaving.look} still={leaving.still} phase="out" />}
      {/* Grows in as it appears - on the page, or over the look it replaces. */}
      <OrbLayer key={shown.key} look={shown.look} still={shown.still} phase={shown.still ? undefined : 'in'} />
    </span>
  )
}

/** One orb drawn into its own svg, for as long as the layer is there. */
function OrbLayer({ look, still, phase }: { look: OrbLook; still: boolean; phase?: 'in' | 'out' }) {
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
      className={cn('n-orb', still && 'is-still', phase && `is-${phase}`)}
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
/**
 * The model at work, a failed request being tried again, or an answer that
 * stopped coming part-way through and is still being written (`writing`).
 */
export function ThinkingLine({ retrying = false, writing = false }: { retrying?: boolean; writing?: boolean }) {
  const { t } = useTranslation()
  return (
    <div className="n-thinking" role="status">
      <ThinkingOrb look={retrying ? RETRYING : WORKING} />
      <span className="n-reasoning-live" data-motion="essential">
        {t(retrying ? 'chat.retrying' : writing ? 'chat.still-writing' : 'chat.reasoning.live')}
      </span>
    </div>
  )
}
