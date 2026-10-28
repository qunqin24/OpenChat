import type { FoundImage } from '@openchat/api-client'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { smoothScrollBehavior } from '@/lib/motion'
import { siteName } from '@/lib/urls'
import { cn } from '@/lib/utils'
import { credit, type ShownPicture } from './found-image'


/**
 * A picture (usePicture) that removes itself, and lets its frame know, when
 * its bytes will not load. A found photo's creator and licence are its tooltip.
 */
export function Picture({
  picture,
  alt,
  className,
  onMissing,
}: {
  picture: ShownPicture
  alt: string
  className?: string
  onMissing?: () => void
}) {
  const [failed, setFailed] = useState(false)
  if (failed) return null
  return (
    <img
      className={className}
      src={picture.src}
      alt={alt || picture.found?.title || ''}
      title={credit(picture.found) || undefined}
      loading="lazy"
      decoding="async"
      onError={() => {
        setFailed(true)
        onMissing?.()
      }}
    />
  )
}

/**
 * Where a found photo is from and under which licence, as a chip laid over it
 * - what a Creative Commons licence asks to be shown with the picture.
 */
export function PictureCredit({ found }: { found: FoundImage }) {
  const text = [found.source, found.license].filter(Boolean).join(' · ')
  return text ? <span className="n-ui-source-chip is-credit">{text}</span> : null
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
