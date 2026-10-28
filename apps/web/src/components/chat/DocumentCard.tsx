import type { ToolCallPart } from '@openchat/api-client'
import { useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Maximize2, NotebookPen } from 'lucide-react'
import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useDocument } from '@/hooks/use-documents'
import { queryKeys } from '@/lib/query-keys'
import { copyClipboard } from '@/lib/clipboard'
import { changedPassages, pendingEditOf, pendingTitle, shownText, streamingText, type DocumentChange } from '@/lib/document-tool'
import { cn } from '@/lib/utils'
import { useDocumentActivity } from '@/stores/document-activity-store'
import { useDocumentPanel } from '@/stores/document-panel-store'
import { Markdown } from './MarkdownText'
import { ThinkingOrb } from './ThinkingOrb'

/**
 * A writing block in a reply (the `document` tool): the text as this call
 * left it - title, text, copy and open beside the chat. Where it created the
 * document, the whole; where it changed it, the paragraphs it changed, or the
 * whole where most of it changed. While the call is still being written, the
 * words as they come (WritingCard). Once open in the side panel, the card
 * shrinks to its name, as the text is there.
 */
export function DocumentCard({ change, call }: { change: DocumentChange | null; call: ToolCallPart }) {
  const { t } = useTranslation()
  // An update still being written: which document, and which passages, so the
  // panel can mark them until the new version is in.
  const pending = change ? null : pendingEditOf(call)
  const doc = useDocument(change?.documentId ?? pending?.documentId ?? null, change?.version ?? 0)
  const panel = useDocumentPanel()
  const { put, drop } = useDocumentActivity()
  const card = useId()
  const pendingKey = pending ? JSON.stringify(pending) : ''
  useEffect(() => {
    if (pendingKey) put(card, JSON.parse(pendingKey) as NonNullable<typeof pending>)
  }, [pendingKey, card, put])
  const loaded = doc.data?.versions.at(-1)?.version ?? 0
  // A document written or changed: the conversation's list (DocumentsMenu)
  // has a new one, or a new order.
  const queryClient = useQueryClient()
  const listOf = doc.data?.conversationId
  useEffect(() => {
    if (change && listOf) void queryClient.invalidateQueries({ queryKey: queryKeys.conversationDocuments(listOf) })
  }, [change, listOf, queryClient])
  useEffect(() => {
    if (change && loaded >= change.version) drop(card)
  }, [change, loaded, card, drop])
  useEffect(() => () => drop(card), [card, drop])
  // The model starting on a document opens it beside the chat, once, so the
  // change can be watched as it is made.
  const opened = useRef(false)
  const conversationOf = doc.data?.conversationId
  useEffect(() => {
    if (!pending || !conversationOf || opened.current) return
    opened.current = true
    const state = useDocumentPanel.getState()
    if (state.documentId !== pending.documentId) state.open(conversationOf, pending.documentId)
  }, [pending, conversationOf])
  const [copied, setCopied] = useState(false)
  // The fade at the foot of the card only where the text runs on past it.
  const body = useRef<HTMLDivElement>(null)
  const [cut, setCut] = useState(false)
  useLayoutEffect(() => {
    const el = body.current
    if (!el) return
    const measure = () => setCut(el.scrollHeight > el.clientHeight + 1)
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    return () => observer.disconnect()
  })

  if (!change) {
    const name = pending ? doc.data?.versions.at(-1)?.title ?? '' : pendingTitle(call)
    // An update reads as one from its op, before its document's id has come.
    const updating = pending !== null || /"op"\s*:\s*"update"/.test(call.rawArguments ?? '')
    return (
      <WritingCard
        label={t(updating ? 'document.rewriting' : 'document.writing')}
        title={name}
        text={streamingText(call)}
        onOpen={pending && doc.data ? () => doc.data && panel.open(doc.data.conversationId, pending.documentId) : undefined}
      />
    )
  }

  // This call's version, and the one it changed.
  const versions = doc.data?.versions
  const made = versions?.find((v) => v.version === change.version) ?? versions?.at(-1)
  const before = change.op === 'update' && made ? versions?.find((v) => v.version === made.version - 1) : undefined
  const title = made?.title ?? change.title
  const label = t(change.op === 'update' ? 'document.updated' : 'document.label')
  const open = () => doc.data && panel.open(doc.data.conversationId, change.documentId)
  const shown = panel.documentId === change.documentId

  if (shown) {
    // A pill fitting its words: the text is in the panel. The one whose
    // version the panel shows is marked, and each opens the panel at its own.
    const last = versions?.at(-1)?.version
    const at = made?.version ?? change.version
    const on = (panel.viewing ?? last) === at
    return (
      <button
        type="button"
        className={cn('n-doc-pill', on && 'is-on')}
        aria-pressed={on}
        onClick={() => doc.data && panel.open(doc.data.conversationId, change.documentId, at === last ? null : at)}
        disabled={!doc.data}
      >
        <NotebookPen aria-hidden="true" />
        <span className="n-doc-pill-label">{t(change.op === 'update' ? 'document.pill-updated' : 'document.label')}</span>
        <span className="n-doc-pill-title" dir="auto">{title}</span>
        {at > 0 && <span className="n-doc-pill-version">{t('document.pill-version', { version: at })}</span>}
      </button>
    )
  }

  const copy = async () => {
    if (!made) return
    if (!(await copyClipboard(made.content))) return
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1500)
  }

  return (
    <section className="n-doc-card" aria-label={title}>
      <header className="n-doc-card-head">
        <span className="n-doc-card-label">{label}</span>
        <div className="n-doc-card-actions">
          <button type="button" className="n-icon-btn" aria-label={t('document.copy')} title={t('document.copy')} onClick={() => void copy()} disabled={!made}>
            {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
          </button>
          <button type="button" className="n-icon-btn" aria-label={t('document.open')} title={t('document.open')} onClick={open} disabled={!doc.data}>
            <Maximize2 className="h-4 w-4" />
          </button>
        </div>
      </header>
      {/* The text opens the panel on a click too; the button above is the keyboard's way. */}
      <div ref={body} className={cn('n-doc-card-body', cut && 'is-cut')} onClick={open}>
        <h3 className="n-doc-card-title" dir="auto">{title}</h3>
        {made ? (
          changedPassages(before?.content, made.content).map((part, i) =>
            part === true ? <div key={i} className="n-doc-card-gap" aria-hidden="true">⋯</div> : <Markdown key={i} text={part} />,
          )
        ) : doc.isError ? <p className="n-doc-card-gone">{t('document.gone')}</p> : null}
      </div>
    </section>
  )
}

/** How much of the text in flight the card lays out: more than its window shows. */
const LIVE_TAIL = 1200

/**
 * A create or update still being written: its title, and the words as they
 * stream in - the newest at the foot of a short window, the older ones
 * fading out over its top - with a count of what has come. Before the first
 * word, bars of placeholder. Plain paragraphs, not Markdown: it changes with
 * every delta, and the finished card draws it properly.
 */
function WritingCard({ label, title, text, onOpen }: { label: string; title: string; text: string; onOpen?: () => void }) {
  const { t } = useTranslation()
  const count = useMemo(() => text.replace(/\s/g, '').length, [text])
  const lines = useMemo(() => {
    const plain = shownText(text)
    return plain.slice(-LIVE_TAIL).split(/\n+/).filter((line) => line.trim())
  }, [text])
  // The fade over the top only once the text has filled the window.
  const live = useRef<HTMLDivElement>(null)
  const flow = useRef<HTMLDivElement>(null)
  const [full, setFull] = useState(false)
  useLayoutEffect(() => {
    if (live.current && flow.current) setFull(flow.current.offsetHeight > live.current.clientHeight + 1)
  }, [lines])

  return (
    <section className={cn('n-doc-card is-writing', onOpen && 'is-openable')} role="status" aria-label={title || label} onClick={onOpen}>
      <header className="n-doc-card-head">
        <span className="n-doc-card-label">
          <ThinkingOrb />
          <span className="n-reasoning-live" data-motion="essential">{label}</span>
        </span>
        {count > 0 && <span className="n-doc-card-count">{t('document.count', { count })}</span>}
      </header>
      <div className="n-doc-card-body">
        {title && <h3 className="n-doc-card-title" dir="auto">{title}</h3>}
        {lines.length > 0 ? (
          <div ref={live} className={cn('n-doc-live', full && 'is-full')}>
            <div ref={flow} className="n-doc-live-flow">
              {lines.map((line, i) => (
                <p key={i} dir="auto">
                  {line}
                  {i === lines.length - 1 && <span className="n-doc-caret" data-motion="essential" aria-hidden="true" />}
                </p>
              ))}
            </div>
          </div>
        ) : (
          <div className="n-doc-live-bars" aria-hidden="true">
            <span data-motion="essential" />
            <span data-motion="essential" />
            <span data-motion="essential" />
          </div>
        )}
      </div>
    </section>
  )
}

export default DocumentCard
