import { ArrowUpRight, Check, Copy } from 'lucide-react'
import { Popover } from 'radix-ui'
import { useEffect, useRef, useState, type ComponentProps, type PointerEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { copyClipboard } from '@/lib/clipboard'
import { isHttpUrl } from '@openchat/chat-core/urls'
import { SourceMark } from './ToolActivity'

type LinkProps = ComponentProps<'a'> & { node?: unknown }

const OPEN_DELAY = 300
const CLOSE_DELAY = 120

/**
 * A link in an answer: its words with a dotted underline and a small arrow
 * after them, so a link reads as one without shouting in running text, and a
 * card on hover with the site's icon, the whole address, and buttons to open
 * or copy it - what the words alone do not say is where they go.
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
          later(true)
        }}
        onPointerMove={pick}
        onPointerLeave={(event) => event.pointerType === 'mouse' && later(false)}
        onFocus={() => {
          line.current = 0
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
          <LinkCard url={href} />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  )
}

/** What a virtual anchor has to offer: where it is. */
type Measurable = { getBoundingClientRect: () => DOMRect }

function LinkCard({ url }: { url: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const timer = useRef(0)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  const copy = async () => {
    if (!(await copyClipboard(url))) return
    setCopied(true)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setCopied(false), 1500)
  }
  return (
    <>
      <SourceMark url={url} />
      <a className="n-link-card-url" href={url} rel="noreferrer" target="_blank" title={url}>
        {url}
      </a>
      <a className="n-link-card-button" href={url} rel="noreferrer" target="_blank" aria-label={t('chat.link-open')}>
        <ArrowUpRight aria-hidden="true" />
      </a>
      <button type="button" className="n-link-card-button" onClick={() => void copy()} aria-label={copied ? t('chat.link-copied') : t('chat.link-copy')}>
        {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
      </button>
    </>
  )
}

export default MarkdownLink
