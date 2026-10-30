import { useEffect, useRef, useState, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'

import type { OutlineEntry } from '@openchat/chat-core/outline'
import { smoothScrollBehavior } from '@/lib/motion'
import { cn } from '@/lib/utils'

/** A conversation shorter than this is a scroll or two: nothing to navigate. */
const MIN_TURNS = 2
/**
 * Room the navigator needs between the column and the transcript's right
 * edge: its lines, and the nearness to their left where the pointer draws
 * them out (`.n-outline`'s padding), clear of the text.
 */
const ROOM = 88
/** How far below the transcript's top a question is taken as being read. */
const READING_LINE = 96

/**
 * The conversation at a glance, at the transcript's right edge: a short line
 * for each thing the user said, the one being read longer and darker. Pointing
 * at a line shows that question and the start of its answer; clicking scrolls
 * to it. Each user message is marked with its turn (`data-turn`); only loaded
 * turns are listed (the transcript pages from its end). Hidden where the
 * margin beside the column is too narrow for it.
 */
export function TurnNavigator({ entries, scrollRef }: { entries: OutlineEntry[]; scrollRef: RefObject<HTMLDivElement | null> }) {
  const { t } = useTranslation()
  const [current, setCurrent] = useState<string | null>(null)
  const [roomy, setRoomy] = useState(false)
  const [card, setCard] = useState<{ entry: OutlineEntry; top: number } | null>(null)
  const nav = useRef<HTMLElement>(null)
  const rail = useRef<HTMLDivElement>(null)
  const listed = entries.length >= MIN_TURNS

  // Which turn is being read: the last whose question has passed the reading
  // line, or the last of all once the transcript is at its end.
  useEffect(() => {
    const scroller = scrollRef.current
    if (!scroller || !listed) return
    let frame = 0
    const check = () => {
      frame = 0
      const marks = scroller.querySelectorAll<HTMLElement>('[data-turn]')
      if (marks.length === 0) return
      const line = scroller.getBoundingClientRect().top + parseFloat(getComputedStyle(scroller).paddingTop) + READING_LINE
      let key = marks[0]?.dataset.turn
      for (const mark of marks) {
        if (mark.getBoundingClientRect().top > line) break
        key = mark.dataset.turn
      }
      if (scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 4) key = marks[marks.length - 1]?.dataset.turn
      setCurrent(key ?? null)
    }
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(check)
    }
    check()
    scroller.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      scroller.removeEventListener('scroll', onScroll)
      if (frame) cancelAnimationFrame(frame)
    }
  }, [scrollRef, entries, listed])

  // Only where it fits beside the column, clear of the text.
  useEffect(() => {
    const scroller = scrollRef.current
    if (!scroller || !listed) return
    const measure = () => {
      const column = scroller.querySelector<HTMLElement>('.n-column')
      setRoomy(column !== null && (scroller.clientWidth - column.offsetWidth) / 2 >= ROOM)
    }
    const observer = new ResizeObserver(measure)
    observer.observe(scroller)
    return () => observer.disconnect()
  }, [scrollRef, listed])

  // A long conversation scrolls its lines: keep the one being read in view.
  useEffect(() => {
    const box = rail.current
    const tick = box?.querySelector<HTMLElement>('[aria-current]')
    if (!box || !tick) return
    if (tick.offsetTop < box.scrollTop) box.scrollTop = tick.offsetTop
    else if (tick.offsetTop + tick.offsetHeight > box.scrollTop + box.clientHeight) box.scrollTop = tick.offsetTop + tick.offsetHeight - box.clientHeight
  }, [current, roomy])

  if (!listed || !roomy) return null

  const show = (entry: OutlineEntry, tick: HTMLElement) => {
    const frame = nav.current?.getBoundingClientRect()
    if (!frame) return
    const at = tick.getBoundingClientRect()
    setCard({ entry, top: at.top + at.height / 2 - frame.top })
  }
  const jump = (key: string) => {
    const scroller = scrollRef.current
    const mark = scroller?.querySelector<HTMLElement>(`[data-turn="${CSS.escape(key)}"]`)
    if (!scroller || !mark) return
    const top = scroller.scrollTop + mark.getBoundingClientRect().top - scroller.getBoundingClientRect().top - parseFloat(getComputedStyle(scroller).paddingTop) - 16
    scroller.scrollTo({ top, behavior: smoothScrollBehavior() })
  }

  return (
    <nav ref={nav} className="n-outline" aria-label={t('chat.outline')} onMouseLeave={() => setCard(null)}>
      <div ref={rail} className="n-outline-rail">
        {entries.map((entry) => (
          <button
            key={entry.key}
            type="button"
            className={cn('n-outline-tick', entry.key === current && 'is-current')}
            aria-label={entry.question}
            aria-current={entry.key === current ? 'location' : undefined}
            onMouseEnter={(event) => show(entry, event.currentTarget)}
            onFocus={(event) => show(entry, event.currentTarget)}
            onBlur={() => setCard(null)}
            onClick={() => jump(entry.key)}
          >
            <span />
          </button>
        ))}
      </div>
      {card && (
        <div className="n-outline-card" style={{ top: card.top }} aria-hidden="true">
          <div className="n-outline-question">{card.entry.question}</div>
          {card.entry.answer && <div className="n-outline-answer">{card.entry.answer}</div>}
        </div>
      )}
    </nav>
  )
}
