import type { ReasoningEffort } from '@openchat/api-client'
import { useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

/** Distance between two stops, and the track's height (its ends are half that). */
const STEP = 26
const HEIGHT = 28
const KNOB = 22
const KNOB_DRAG = 28
const INSET = (HEIGHT - KNOB) / 2

interface ThinkingSliderProps {
  /** The model's own levels, lowest first, from the catalogue. */
  levels: ReasoningEffort[]
  value: ReasoningEffort
  onChange: (effort: ReasoningEffort) => void
  /** The level under the knob while it is dragged, null once released. */
  onPreview?: (effort: ReasoningEffort | null) => void
  disabled?: boolean
}

/**
 * How hard the model thinks, as a stepped slider: automatic at the far left,
 * then the model's own levels rising to the right. Drag or click to a stop, or
 * use the arrow keys. The level's name is shown by whoever hosts it, through
 * `onPreview` while dragging.
 *
 * The choice is committed on release, not on every stop passed, so dragging
 * across five levels is one change to the conversation rather than five.
 */
export function ThinkingSlider({ levels, value: stored, onChange, onPreview, disabled }: ThinkingSliderProps) {
  const { t } = useTranslation()
  const stops: ReasoningEffort[] = ['auto', ...levels]
  // A level remembered from another model that this one does not have is not
  // sent (the server drops it too), so it sits at automatic here.
  const index = Math.max(0, stops.indexOf(stored))
  const last = stops.length - 1
  const width = HEIGHT + STEP * last

  const trackRef = useRef<HTMLDivElement>(null)
  // A fractional stop while dragging, so the knob follows the pointer; null at rest.
  const [dragPos, setDragPos] = useState<number | null>(null)
  const dragging = dragPos !== null
  const pos = dragPos ?? index

  const stopAt = (clientX: number) => {
    const rect = trackRef.current?.getBoundingClientRect()
    if (!rect) return index
    const t = (clientX - rect.left - HEIGHT / 2) / (rect.width - HEIGHT)
    return Math.min(last, Math.max(0, t * last))
  }

  const drag = (next: number) => {
    setDragPos(next)
    onPreview?.(stops[Math.round(next)] ?? 'auto')
  }

  const commit = (next: number) => {
    const effort = stops[Math.round(next)]
    if (effort && effort !== stops[index]) onChange(effort)
  }

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (disabled || event.button !== 0) return
    event.currentTarget.setPointerCapture(event.pointerId)
    drag(stopAt(event.clientX))
  }
  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    if (dragPos !== null) drag(stopAt(event.clientX))
  }
  const onPointerUp = (event: PointerEvent<HTMLDivElement>) => {
    if (dragPos === null) return
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
    commit(dragPos)
    setDragPos(null)
    onPreview?.(null)
  }
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (disabled) return
    const next =
      event.key === 'ArrowRight' || event.key === 'ArrowUp'
        ? Math.min(last, index + 1)
        : event.key === 'ArrowLeft' || event.key === 'ArrowDown'
          ? Math.max(0, index - 1)
          : event.key === 'Home'
            ? 0
            : event.key === 'End'
              ? last
              : null
    if (next === null) return
    event.preventDefault()
    commit(next)
  }

  // The knob widens while held, and never leaves the track at either end.
  const knob = dragging ? KNOB_DRAG : KNOB
  const center = HEIGHT / 2 + pos * STEP
  const knobLeft = Math.min(width - INSET - knob, Math.max(INSET, center - knob / 2))
  const fill = knobLeft + knob + INSET
  // Automatic is no level at all, so it shows no fill — dragging included.
  const empty = Math.round(pos) === 0

  return (
    <div
      className={cn('n-think', dragging && 'is-dragging')}
      style={{ width }}
    >
      <div
        ref={trackRef}
        className="n-think-track"
        role="slider"
        tabIndex={disabled ? -1 : 0}
        aria-label={t('chat.thinking.heading')}
        aria-valuemin={0}
        aria-valuemax={last}
        aria-valuenow={index}
        aria-valuetext={t(`chat.thinking.${stops[index] ?? 'auto'}`)}
        aria-disabled={disabled}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        onKeyDown={onKeyDown}
      >
        <span className={cn('n-think-fill', empty && 'is-empty')} style={{ width: fill }} />
        {/* Before the ticks, so the one it covers shows on top of it. */}
        <span className="n-think-knob" style={{ left: knobLeft, width: knob }} />
        {stops.map((stop, i) => {
          const x = HEIGHT / 2 + i * STEP
          const under = x >= knobLeft && x <= knobLeft + knob
          return (
            <span
              key={stop}
              className={cn('n-think-tick', !empty && x < knobLeft && 'is-filled', under && 'is-under')}
              // Taller to the right: more thinking.
              style={{ left: x, height: 4 + (6 * i) / Math.max(1, last) }}
            />
          )
        })}
      </div>
    </div>
  )
}
