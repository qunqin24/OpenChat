import { ChevronDown, ChevronUp, MessageSquareQuote, NotebookPen, X } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { highlightPassage } from '@/lib/highlight-passage'
import { cn } from '@/lib/utils'
import { quotePreview } from '@openchat/chat-core/quote-preview'

/**
 * Quoted passages, as a mail client quotes (design B, 2026-11-03): a thin grey
 * bar on the left and the passage as text - its Markdown read, not shown
 * (quotePreview) - with no box around it. One passage shows two lines and
 * where it is from; several are a numbered line each, so the question can say
 * "the second", and past three the rest fold under "N more".
 */
export interface Quote {
  title: string
  passage: string
  /** A passage of an answer (else of a writing block, `title` naming it). */
  fromAnswer?: boolean
}

/** How many passages the composer lists before folding the rest. */
const SHOWN = 3

/** Where a quote is from: "回答选段", or the writing block's title. */
function Source({ quote }: { quote: Quote }) {
  const { t } = useTranslation()
  return (
    <span className="n-qb-src">
      {quote.fromAnswer ? <MessageSquareQuote aria-hidden="true" /> : <NotebookPen aria-hidden="true" />}
      <span>{quote.fromAnswer ? t('chat.quote-label') : quote.title || t('document.quote-label')}</span>
    </span>
  )
}

/**
 * The passages waiting in the composer, each with its remove. A passage of an
 * answer is shown where it is at a click on it (`onShow`), and again to hide.
 */
export function ComposerQuotes({
  quotes,
  shown,
  onShow,
  onRemove,
}: {
  quotes: Quote[]
  shown: string | null
  onShow: (passage: string) => void
  onRemove: (index: number) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  if (quotes.length === 0) return null

  const remove = (i: number) => (
    <button type="button" className="n-qb-remove" aria-label={t(quotes[i]!.fromAnswer ? 'chat.quote-remove' : 'document.quote-remove')} onClick={() => onRemove(i)}>
      <X aria-hidden="true" />
    </button>
  )
  const body = (quote: Quote, children: React.ReactNode) =>
    quote.fromAnswer ? (
      <button
        type="button"
        className={cn('n-qb-body is-showable', shown === quote.passage && 'is-shown')}
        aria-pressed={shown === quote.passage}
        aria-label={t('chat.quote-show')}
        onClick={() => onShow(quote.passage)}
      >
        {children}
      </button>
    ) : (
      <div className="n-qb-body">{children}</div>
    )

  if (quotes.length === 1) {
    const quote = quotes[0]!
    return (
      <div className="n-qb-tray">
        <div className="n-qb-item">
          {body(
            quote,
            <span className="n-qb is-one">
              <span className="n-qb-text" dir="auto">{quotePreview(quote.passage)}</span>
              <Source quote={quote} />
            </span>,
          )}
          {remove(0)}
        </div>
      </div>
    )
  }

  const listed = open ? quotes : quotes.slice(0, SHOWN)
  return (
    <div className="n-qb-tray">
      {listed.map((quote, i) => (
        <div key={`${i}-${quote.passage.slice(0, 24)}`} className="n-qb-item">
          <span className="n-qb-num" aria-hidden="true">{i + 1}</span>
          {body(
            quote,
            <span className="n-qb">
              <span className="n-qb-text is-line" dir="auto">{quotePreview(quote.passage)}</span>
            </span>,
          )}
          {remove(i)}
        </div>
      ))}
      {quotes.length > SHOWN && (
        <button type="button" className="n-qb-more" aria-expanded={open} onClick={() => setOpen(!open)}>
          {open ? <ChevronUp aria-hidden="true" /> : <ChevronDown aria-hidden="true" />}
          {open ? t('chat.quote-less') : t('chat.quote-more', { count: quotes.length - SHOWN })}
        </button>
      )}
    </div>
  )
}

/**
 * A sent message's passages, inside its bubble above its words: one bar for
 * all of them, a numbered line each when there are several, where they are
 * from last. A passage of an answer is shown where it is at a click.
 */
export function SentQuotes({ quotes }: { quotes: Quote[] }) {
  const { t } = useTranslation()
  const [shown, setShown] = useState<string | null>(null)
  if (quotes.length === 0) return null

  const show = (passage: string) => {
    const next = shown !== passage && highlightPassage(passage) ? passage : null
    if (!next) highlightPassage(null)
    setShown(next)
  }
  const line = (quote: Quote, i: number) => {
    const text = (
      <>
        {quotes.length > 1 && <span className="n-qb-num" aria-hidden="true">{i + 1}</span>}
        <span className={cn('n-qb-text', quotes.length > 1 && 'is-line')} dir="auto">{quotePreview(quote.passage)}</span>
      </>
    )
    return quote.fromAnswer ? (
      <button
        key={i}
        type="button"
        className={cn('n-qb-row is-showable', shown === quote.passage && 'is-shown')}
        aria-pressed={shown === quote.passage}
        aria-label={t('chat.quote-show')}
        onClick={() => show(quote.passage)}
      >
        {text}
      </button>
    ) : (
      <span key={i} className="n-qb-row">{text}</span>
    )
  }

  return (
    <div className="n-qb is-sent">
      {quotes.map(line)}
      <Source quote={quotes[0]!} />
    </div>
  )
}
