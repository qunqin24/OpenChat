import { MessageSquareQuote } from 'lucide-react'
import { useEffect, useRef, useState, type RefObject } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'

/** The longest passage quoted; a longer selection is cut there. */
const MAX_PASSAGE = 4000

/**
 * "引用提问" over text selected in an answer: a small button above the start
 * of the selection that puts the passage in the composer as a quote, for the
 * question the person then types about it (lib/document-selection, sent as a
 * <quote> before their words). Only a selection within one answer of this
 * conversation's transcript gets it - not the person's own messages, not the
 * composer, not across two answers.
 *
 * Drawn in a portal under <body> and placed in the viewport, so no scroller or
 * transformed ancestor clips or shifts it; its styles are therefore not scoped
 * to .chat-ui (agent.css). It waits until the pointer is up, so it does not
 * chase a selection being dragged.
 */
export function AskSelection({ scope, onAsk }: { scope: RefObject<HTMLElement | null>; onAsk: (passage: string) => void }) {
  const { t } = useTranslation()
  const [spot, setSpot] = useState<{ x: number; y: number; passage: string } | null>(null)
  const pressing = useRef(false)

  useEffect(() => {
    let frame = 0
    const measure = () => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => {
        if (pressing.current) return
        setSpot(selectedPassage(scope.current))
      })
    }
    const down = (event: PointerEvent) => {
      // A press on the button itself keeps the selection and the button.
      if (event.target instanceof Element && event.target.closest('.n-ask-bubble')) return
      pressing.current = true
      setSpot(null)
    }
    const up = () => {
      pressing.current = false
      measure()
    }
    document.addEventListener('selectionchange', measure)
    document.addEventListener('pointerdown', down, true)
    document.addEventListener('pointerup', up, true)
    // The transcript scrolls under a selection: the button follows it.
    document.addEventListener('scroll', measure, true)
    window.addEventListener('resize', measure)
    return () => {
      cancelAnimationFrame(frame)
      document.removeEventListener('selectionchange', measure)
      document.removeEventListener('pointerdown', down, true)
      document.removeEventListener('pointerup', up, true)
      document.removeEventListener('scroll', measure, true)
      window.removeEventListener('resize', measure)
    }
  }, [scope])

  if (!spot) return null
  return createPortal(
    <button
      type="button"
      className="n-ask-bubble"
      style={{ left: spot.x, top: spot.y }}
      // Pressing would otherwise collapse the selection before the click.
      onPointerDown={(event) => event.preventDefault()}
      onClick={() => {
        onAsk(spot.passage)
        window.getSelection()?.removeAllRanges()
        setSpot(null)
      }}
    >
      <MessageSquareQuote aria-hidden="true" />
      {t('chat.ask-selection')}
    </button>,
    document.body,
  )
}

/**
 * The selected passage of one answer inside scope, and where its button goes:
 * above the start of the selection, kept inside the viewport. Null for no
 * selection, one outside an answer, or one spanning two answers.
 */
function selectedPassage(scope: HTMLElement | null): { x: number; y: number; passage: string } | null {
  const selection = window.getSelection()
  if (!scope || !selection || selection.isCollapsed || selection.rangeCount === 0) return null
  const answer = answerOf(selection.anchorNode)
  if (!answer || answer !== answerOf(selection.focusNode) || !scope.contains(answer)) return null
  const passage = selection.toString().trim().slice(0, MAX_PASSAGE)
  if (!passage) return null
  const range = selection.getRangeAt(0)
  const first = [...range.getClientRects()].find((rect) => rect.width > 0) ?? range.getBoundingClientRect()
  if (first.bottom < 0 || first.top > window.innerHeight) return null
  const x = Math.min(Math.max(8, first.left), window.innerWidth - 160)
  // Above the first line; below it when the top of the window is too close.
  const y = first.top > 52 ? first.top - 44 : first.bottom + 8
  return { x, y, passage }
}

function answerOf(node: Node | null): Element | null {
  const element = node instanceof Element ? node : (node?.parentElement ?? null)
  return element?.closest('.n-msg-assistant') ?? null
}

export default AskSelection
