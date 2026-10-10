import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useContext, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { smoothScrollBehavior } from '@/lib/motion'
import { siteName } from '@/lib/urls'
import { cn } from '@/lib/utils'
import { ImageSource } from './image-source'


/** A source picture that removes itself, and lets its frame know, when it cannot load. */
export function Picture({
  src,
  alt,
  className,
  onMissing,
}: {
  src: string
  alt: string
  className?: string
  onMissing?: () => void
}) {
  const resolve = useContext(ImageSource)
  const [failed, setFailed] = useState(false)
  const url = src ? resolve(src) : null
  useLayoutEffect(() => {
    if (!url) onMissing?.()
  }, [url, onMissing])
  if (!url || failed) return null
  return (
    <img
      className={className}
      src={url}
      alt={alt}
      loading="lazy"
      decoding="async"
      onError={() => {
        setFailed(true)
        onMissing?.()
      }}
    />
  )
}

/** The site a picture or story came from, as a chip laid over the picture. */
export function SourceChip({ link }: { link?: string }) {
  const site = link ? siteName(link) : ''
  return site ? <span className="n-ui-source-chip">{site}</span> : null
}

/** Opens `link` in a new tab when there is one; otherwise just its content. */
export function MaybeLink({ link, className, children }: { link?: string; className?: string; children: ReactNode }) {
  return /^https?:\/\//.test(link ?? '') ? (
    <a className={className} href={link} target="_blank" rel="noreferrer">
      {children}
    </a>
  ) : (
    <div className={className}>{children}</div>
  )
}

/**
 * A row the reader scrolls sideways, snapping to each item, with arrow buttons
 * on a pointer device once there is more than fits.
 */
export function Scroller({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const track = useRef<HTMLDivElement>(null)
  const [edges, setEdges] = useState({ start: true, end: true })

  useLayoutEffect(() => {
    const el = track.current
    if (!el) return
    const measure = () =>
      setEdges({ start: el.scrollLeft <= 2, end: el.scrollLeft + el.clientWidth >= el.scrollWidth - 2 })
    measure()
    el.addEventListener('scroll', measure, { passive: true })
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    return () => {
      el.removeEventListener('scroll', measure)
      observer.disconnect()
    }
  }, [])

  const page = (direction: 1 | -1) => {
    const el = track.current
    if (el) el.scrollBy({ left: direction * el.clientWidth * 0.8, behavior: smoothScrollBehavior() })
  }

  return (
    <div className="n-ui-scroller">
      <div ref={track} className="n-ui-scroller-track">
        {children}
      </div>
      <button
        type="button"
        className={cn('n-ui-scroller-arrow is-prev', edges.start && 'is-hidden')}
        aria-label={t('chat.genui.previous')}
        tabIndex={-1}
        onClick={() => page(-1)}
      >
        <ChevronLeft aria-hidden="true" />
      </button>
      <button
        type="button"
        className={cn('n-ui-scroller-arrow is-next', edges.end && 'is-hidden')}
        aria-label={t('chat.genui.next')}
        tabIndex={-1}
        onClick={() => page(1)}
      >
        <ChevronRight aria-hidden="true" />
      </button>
    </div>
  )
}
