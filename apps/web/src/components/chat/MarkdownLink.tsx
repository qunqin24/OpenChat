import { ArrowUpRight, Check, Copy } from 'lucide-react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Popover } from 'radix-ui'
import { useContext, useEffect, useRef, useState, type ComponentProps, type PointerEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { copyClipboard } from '@/lib/clipboard'
import { isHttpUrl, siteName } from '@openchat/chat-core/urls'
import { LinkPreviews, linkPreviewImage, linkPreviewQuery } from './link-preview'
import { SourceMark } from './ToolActivity'

type LinkProps = ComponentProps<'a'> & { node?: unknown }

const OPEN_DELAY = 300
const CLOSE_DELAY = 120

/**
 * A link in an answer: its words with a dotted underline and a small arrow
 * after them, so a link reads as one without shouting in running text, and a
 * card on hover with the page's sharing picture, its site and title, and
 * buttons to open or copy it - what the words alone do not say is where they
 * go. The page is read by the server (link-preview.ts), which reads only
 * pages this conversation's tool results list; for any other link, and until
 * the page is read, the card shows the address in the title's place.
 *
 * The card stands over the line the pointer is on. A link that wraps is two
 * boxes, and the box round both starts at the left of the lower line, far
 * from the words under the pointer, so the card is anchored to the hovered
 * line's own box (a Popover's virtual anchor; HoverCard cannot take one) and
 * opens and closes on hover by the same delays a HoverCard has.
 *
 * Anything but http(s), and the half-written link Streamdown marks while an
 * answer streams, is the plain link Streamdown would have drawn.
 */
export function MarkdownLink({ href, children, node: _node, className, ...rest }: LinkProps) {
  const link = useRef<HTMLAnchorElement>(null)
  const line = useRef(0)
  const anchor = useRef<Measurable>({
    getBoundingClientRect: () => {
      const rects = link.current?.getClientRects()
      return rects?.[Math.min(line.current, rects.length - 1)] ?? link.current?.getBoundingClientRect() ?? new DOMRect()
    },
  })
  const [open, setOpen] = useState(false)
  const conversationId = useContext(LinkPreviews)
  const queryClient = useQueryClient()
  // Asked for as the pointer arrives, so the delay before the card opens is
  // spent reading the page rather than showing the address first.
  const prefetch = () => {
    if (conversationId && href && isHttpUrl(href)) void queryClient.prefetchQuery(linkPreviewQuery(conversationId, href))
  }
  const timer = useRef(0)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  const later = (next: boolean) => {
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setOpen(next), next ? OPEN_DELAY : CLOSE_DELAY)
  }
  const stay = () => window.clearTimeout(timer.current)
  // The line under the pointer, while the card is shut: once it is open it
  // stays where it opened rather than following the pointer along the link.
  const pick = (event: PointerEvent<HTMLAnchorElement>) => {
    if (open) return
    const rects = [...event.currentTarget.getClientRects()]
    const at = rects.findIndex((rect) => event.clientY >= rect.top && event.clientY <= rect.bottom)
    line.current = Math.max(0, at)
  }

  if (!isHttpUrl(href)) {
    return (
      <a className={className} data-streamdown="link" href={href} rel="noreferrer" target="_blank" {...rest}>
        {children}
      </a>
    )
  }
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Anchor virtualRef={anchor} />
      <a
        ref={link}
        className="n-link"
        data-streamdown="link"
        href={href}
        rel="noreferrer"
        target="_blank"
        {...rest}
        onPointerEnter={(event) => {
          if (event.pointerType !== 'mouse') return
          pick(event)
          prefetch()
          later(true)
        }}
        onPointerMove={pick}
        onPointerLeave={(event) => event.pointerType === 'mouse' && later(false)}
        onFocus={() => {
          line.current = 0
          prefetch()
          later(true)
        }}
        onBlur={() => later(false)}
      >
        {children}
        {/* A word joiner keeps the arrow on the line of the last word. */}
        {'\u2060'}
        <ArrowUpRight className="n-link-arrow" aria-hidden="true" />
      </a>
      <Popover.Portal>
        <Popover.Content
          className="n-link-card"
          side="top"
          align="start"
          sideOffset={6}
          collisionPadding={12}
          onOpenAutoFocus={(event) => event.preventDefault()}
          onCloseAutoFocus={(event) => event.preventDefault()}
          onPointerEnter={stay}
          onPointerLeave={() => later(false)}
        >
          <LinkCard url={href} conversationId={conversationId} />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  )
}

/** What a virtual anchor has to offer: where it is. */
type Measurable = { getBoundingClientRect: () => DOMRect }

function LinkCard({ url, conversationId }: { url: string; conversationId: string | null }) {
  const { t } = useTranslation()
  const preview = useQuery({ ...linkPreviewQuery(conversationId ?? '', url), enabled: conversationId !== null })
  const page = preview.data
  const [pictureFailed, setPictureFailed] = useState(false)
  const [copied, setCopied] = useState(false)
  const timer = useRef(0)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  const copy = async () => {
    if (!(await copyClipboard(url))) return
    setCopied(true)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setCopied(false), 1500)
  }
  const buttons = (
    <>
      <a className="n-link-card-button" href={url} rel="noreferrer" target="_blank" aria-label={t('chat.link-open')}>
        <ArrowUpRight aria-hidden="true" />
      </a>
      <button type="button" className="n-link-card-button" onClick={() => void copy()} aria-label={copied ? t('chat.link-copied') : t('chat.link-copy')}>
        {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
      </button>
    </>
  )
  const site = (
    <div className="n-link-card-site">
      <SourceMark url={url} />
      <span>{page?.siteName || siteName(url)}</span>
    </div>
  )
  const title = (
    <a className={page?.title ? 'n-link-card-title' : 'n-link-card-title is-url'} href={url} rel="noreferrer" target="_blank" title={url}>
      {page?.title || url}
    </a>
  )

  // The richest the page allows: its picture over the site and title; with
  // no picture (or one that will not load), the title and its summary; with
  // no summary either, the site beside the buttons and the title under them.
  if (conversationId && page?.image && !pictureFailed) {
    return (
      <>
        <a className="n-link-card-picture" href={url} rel="noreferrer" target="_blank" tabIndex={-1} aria-hidden="true">
          <img src={linkPreviewImage(conversationId, url)} alt="" decoding="async" onError={() => setPictureFailed(true)} />
        </a>
        <div className="n-link-card-body">
          <div className="n-link-card-text">
            {site}
            {title}
          </div>
          {buttons}
        </div>
      </>
    )
  }
  if (page?.description) {
    return (
      <>
        <div className="n-link-card-summary">
          {site}
          {title}
          <p className="n-link-card-description">{page.description}</p>
        </div>
        <div className="n-link-card-foot">
          <span className="n-link-card-address" title={url}>
            {url.replace(/^https?:\/\//, '')}
          </span>
          {buttons}
        </div>
      </>
    )
  }
  return (
    <div className="n-link-card-compact">
      <div className="n-link-card-row">
        {site}
        {buttons}
      </div>
      {title}
    </div>
  )
}

export default MarkdownLink
