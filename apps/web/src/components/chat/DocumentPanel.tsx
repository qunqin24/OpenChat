import { isOpenChatError } from '@openchat/api-client'
import { Markdown as MarkdownExtension } from '@tiptap/markdown'
import Placeholder from '@tiptap/extension-placeholder'
import { EditorContent, useEditor, type Editor } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import { Check, Copy, Download, Maximize2, Minimize2, NotebookPen, Redo2, Undo2, X } from 'lucide-react'
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useDocument, useSaveDocument } from '@/hooks/use-documents'
import { copyClipboard } from '@/lib/clipboard'
import { passageFor } from '@/lib/document-selection'
import { cn } from '@/lib/utils'
import { blocksTouched } from '@/lib/document-tool'
import { useDocumentActivity } from '@/stores/document-activity-store'
import { useDocumentPanel } from '@/stores/document-panel-store'
import { blockMarksKey, DocumentBlockMarks, type BlockMarks } from './document-marks'
import { useDocumentQuote } from '@/stores/document-quote-store'
import DocumentBubble from './DocumentBubble'

/** How long typing pauses before an edit is saved as a version. */
const SAVE_AFTER_MS = 1200

/**
 * The writing block open beside the chat: its text, edited in place as it is
 * laid out (Tiptap, kept as Markdown), saved as a new version a moment after
 * typing stops. The arrows step back and forth through its versions - the
 * model's and the person's - and an older one can be made the current one
 * again. When the model changes the document while it is open, the new
 * version comes in, unless there is an edit not yet saved, which then meets a
 * conflict and gives way to the model's.
 */
export function DocumentPanel({ documentId }: { documentId: string }) {
  const { t } = useTranslation()
  const doc = useDocument(documentId)
  const save = useSaveDocument(documentId)
  const { close, full, setFull, closing } = useDocumentPanel()
  const versions = doc.data?.versions ?? []
  const latest = versions.at(-1)

  // The version shown when stepping back; null is the latest, which is editable.
  // In the store, as a reply's entry opens the panel at its own version.
  const viewing = useDocumentPanel((s) => s.viewing)
  const setViewing = useDocumentPanel((s) => s.view)
  const shown = viewing === null ? latest : versions.find((v) => v.version === viewing)
  const [title, setTitle] = useState('')
  const [copied, setCopied] = useState(false)
  // The version the editor's text is based on, and whether it has changed since.
  const base = useRef(0)
  const dirty = useRef(false)
  const timer = useRef<number | undefined>(undefined)

  const editor = useEditor({
    extensions: [
      StarterKit.configure({ link: { openOnClick: false } }),
      MarkdownExtension,
      DocumentBlockMarks,
      Placeholder.configure({ placeholder: t('document.placeholder') }),
    ],
    contentType: 'markdown',
    content: '',
    immediatelyRender: false,
    editorProps: { attributes: { class: 'n-doc-prose', 'aria-label': t('document.editor') } },
    // Only the person's typing: content set from a version, or the editor
    // made read-only, is not an edit.
    onUpdate: ({ editor: e }) => {
      if (!e.isEditable) return
      dirty.current = true
      schedule()
    },
  })

  // One save at a time. A second sent while one is out carries the same base
  // version and is refused as a conflict, losing what was typed meanwhile;
  // instead it waits, and the edits made during the first go in the next.
  const saving = useRef<Promise<void> | null>(null)
  const titleRef = useRef(title)
  useEffect(() => {
    titleRef.current = title
  }, [title])
  const flush = useCallback(async () => {
    window.clearTimeout(timer.current)
    while (saving.current) await saving.current
    if (!editor || !dirty.current || !latest || !editor.isEditable) return
    dirty.current = false
    const run = (async () => {
      try {
        const saved = await save.mutateAsync({
          title: titleRef.current.trim() || latest.title,
          content: editor.getMarkdown(),
          baseVersion: base.current,
        })
        base.current = saved.versions.at(-1)?.version ?? base.current
      } catch (error) {
        if (isOpenChatError(error) && error.code === 'version_conflict') {
          toast.info(t('document.conflict'))
          void doc.refetch()
        } else {
          dirty.current = true
          toast.error(t('document.save-failed'))
        }
      }
    })()
    saving.current = run
    try {
      await run
    } finally {
      saving.current = null
    }
  }, [editor, latest, save, doc, t])

  // The latest save (with the latest title), for the editor's callbacks and the timer.
  const flushRef = useRef(flush)
  useEffect(() => {
    flushRef.current = flush
  }, [flush])
  const schedule = () => {
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => void flushRef.current(), SAVE_AFTER_MS)
  }

  // A newer version than the editor's - the first load, the model's change -
  // comes in unless an edit is waiting to be saved.
  useEffect(() => {
    if (!editor || !latest || viewing !== null) return
    if (latest.version === base.current || (dirty.current && base.current !== 0)) return
    const first = base.current === 0
    const before = blockTexts(editor)
    base.current = latest.version
    dirty.current = false
    editor.commands.setContent(latest.content, { contentType: 'markdown', emitUpdate: false })
    setTitle(latest.title)
    // The model's change, as it comes in: what is new glows for a moment.
    if (!first && latest.source === 'model') {
      const old = new Set(before.map((text) => text.trim()))
      const fresh = blockTexts(editor).flatMap((text, i) => (old.has(text.trim()) ? [] : [i]))
      mark(editor, { rewriting: [], fresh })
      window.setTimeout(() => mark(editor, { rewriting: [], fresh: [] }), 2400)
    }
  }, [editor, latest, viewing])

  // The passages a reply is rewriting right now: placeholders over them, and
  // the text not editable until the new version is in.
  const edits = useDocumentActivity((s) => s.edits)
  const inFlight = Object.values(edits).filter((e) => e.documentId === documentId)
  const inFlightKey = JSON.stringify(inFlight)
  useEffect(() => {
    if (!editor) return
    const active = JSON.parse(inFlightKey) as typeof inFlight
    if (active.length === 0) {
      if (viewing === null && !editor.isEditable) editor.setEditable(true, false)
      mark(editor, { rewriting: [], fresh: blockMarksKey.getState(editor.state) ? currentFresh(editor) : [] })
      return
    }
    editor.setEditable(false, false)
    const texts = blockTexts(editor)
    const rewriting = active.some((e) => e.rewrite)
      ? texts.map((_, i) => i)
      : blocksTouched(texts, active.flatMap((e) => e.finds))
    mark(editor, { rewriting, fresh: [] })
  }, [editor, inFlightKey, viewing])

  // An older version is shown as it was, and not editable; stepping back to
  // the latest puts its text back. Only on those steps: run on every refetch,
  // this replaced the text (and the marks on it) while a reply was editing.
  const wasViewing = useRef<number | null>(null)
  useEffect(() => {
    if (!editor) return
    const from = wasViewing.current
    wasViewing.current = viewing
    if (viewing === from) return
    // Stepping away from the latest - an entry in a reply can do that - saves
    // what was typed first, while the text is still editable.
    if (from === null) void flushRef.current()
    // Without emitting an update: Tiptap's default would read as an edit, and
    // save the old version's text as a new one.
    editor.setEditable(viewing === null, false)
    if (viewing !== null && shown) editor.commands.setContent(shown.content, { contentType: 'markdown', emitUpdate: false })
    if (viewing === null && latest && base.current === latest.version && !dirty.current) {
      editor.commands.setContent(latest.content, { contentType: 'markdown', emitUpdate: false })
    }
  }, [editor, viewing, shown, latest])

  // What is not saved yet is, when the panel closes.
  useEffect(() => () => void flushRef.current(), [])

  const index = shown ? versions.indexOf(shown) : -1
  const step = (by: -1 | 1) => {
    const next = versions[index + by]
    if (!next) return
    void flush()
    setViewing(next.version === latest?.version ? null : next.version)
  }

  // "提出修改请求": the selection, as the Markdown it is in the document, goes
  // to the composer as a quote. Saved first, so the model edits what is shown.
  const setQuote = useDocumentQuote((s) => s.set)
  const ask = async () => {
    if (!editor || !doc.data || !editor.markdown) return
    await flush()
    const { from, to } = editor.state.selection
    const md = editor.markdown
    const selected = md.serialize(editor.state.doc.cut(from, to).toJSON())
    const blocks: object[] = []
    editor.state.doc.nodesBetween(from, to, (node, _pos, parent) => {
      if (parent !== editor.state.doc) return true
      blocks.push(node.toJSON())
      return false
    })
    const whole = md.serialize({ type: 'doc', content: blocks })
    const full = editor.getMarkdown()
    const passage = passageFor(full, [selected, whole]) ?? whole.trim()
    if (!passage) return
    setQuote({ conversationId: doc.data.conversationId, documentId, title: title || latest?.title || '', passage })
    // To the composer, for the request. Where the panel covers it - full
    // screen, or a phone - the panel steps aside; the reply reopens it.
    if (window.matchMedia('(max-width: 767px)').matches) close()
    else if (full) setFull(false)
    window.setTimeout(() => document.getElementById('input')?.focus(), 0)
  }

  const restore = async () => {
    if (!shown || !latest) return
    try {
      const saved = await save.mutateAsync({ title: shown.title, content: shown.content, baseVersion: latest.version })
      base.current = 0 // take the restored version as it comes in
      setViewing(null)
      void saved
      toast.success(t('document.restored', { version: shown.version }))
    } catch {
      toast.error(t('document.save-failed'))
    }
  }

  const text = () => (viewing === null && editor ? editor.getMarkdown() : (shown?.content ?? ''))
  const copy = async () => {
    if (!(await copyClipboard(text()))) return
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1500)
  }
  const download = () => {
    const name = (title || shown?.title || 'document').replace(/[\\/:*?"<>|]+/g, ' ').trim() || 'document'
    const blob = new Blob([`# ${title || shown?.title || ''}\n\n${text()}\n`], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `${name}.md`
    link.click()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  }

  return (
    <aside className={cn('n-doc-panel', full && 'is-full', closing && 'is-closing')} aria-label={t('document.panel')}>
      <header className="n-doc-bar">
        <span className="n-doc-kind"><NotebookPen aria-hidden="true" />{t('document.label')}</span>
        <div className="n-doc-tools">
          <button type="button" className="n-icon-btn" aria-label={t('document.previous')} title={t('document.previous')} disabled={index <= 0} onClick={() => step(-1)}>
            <Undo2 className="h-4 w-4" />
          </button>
          <button type="button" className="n-icon-btn" aria-label={t('document.next')} title={t('document.next')} disabled={index < 0 || index >= versions.length - 1} onClick={() => step(1)}>
            <Redo2 className="h-4 w-4" />
          </button>
          <span className="n-doc-sep" aria-hidden="true" />
          <button type="button" className="n-icon-btn" aria-label={t('document.copy')} title={t('document.copy')} disabled={!shown} onClick={() => void copy()}>
            {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
          </button>
          <button type="button" className="n-icon-btn" aria-label={t('document.download')} title={t('document.download')} disabled={!shown} onClick={download}>
            <Download className="h-4 w-4" />
          </button>
          <button type="button" className="n-icon-btn n-doc-full-toggle" aria-label={full ? t('document.exit-full') : t('document.full')} title={full ? t('document.exit-full') : t('document.full')} onClick={() => setFull(!full)}>
            {full ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
          </button>
          <button
            type="button"
            className="n-icon-btn"
            aria-label={t('document.close')}
            title={t('document.close')}
            onClick={() => {
              void flush()
              close()
            }}
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      </header>

      {viewing !== null && shown && (
        <div className="n-doc-banner" role="status">
          <span>{t('document.viewing', { version: shown.version, total: versions.length, who: t(`document.by.${shown.source}`) })}</span>
          <button type="button" className="n-doc-banner-btn" onClick={() => void restore()} disabled={save.isPending}>{t('document.restore')}</button>
          <button type="button" className="n-doc-banner-btn is-quiet" onClick={() => setViewing(null)}>{t('document.latest')}</button>
        </div>
      )}

      <div className="n-doc-scroll">
        {doc.isError ? (
          <p className="n-doc-gone">{t('document.gone')}</p>
        ) : (
          <div className="n-doc-page">
            <DocumentTitle
              value={viewing === null ? title : (shown?.title ?? '')}
              readOnly={viewing !== null}
              label={t('document.title')}
              onChange={(value) => {
                setTitle(value)
                dirty.current = true
                schedule()
              }}
            />
            <EditorContent editor={editor} />
            {editor && <DocumentBubble editor={editor} canAsk={viewing === null} onAsk={() => void ask()} />}
          </div>
        )}
      </div>
      <footer className="n-doc-status" aria-live="polite">
        {save.isPending ? t('document.saving') : shown ? t('document.version', { version: shown.version, total: versions.length }) : ''}
      </footer>
    </aside>
  )
}

/** The text of each top-level block, in order. */
function blockTexts(editor: Editor): string[] {
  const texts: string[] = []
  editor.state.doc.forEach((node) => texts.push(node.textContent))
  return texts
}

function mark(editor: Editor, marks: BlockMarks) {
  editor.view.dispatch(editor.state.tr.setMeta(blockMarksKey, marks).setMeta('addToHistory', false))
}

/** The blocks still glowing from the last change, kept when the placeholders go. */
function currentFresh(editor: Editor): number[] {
  const set = blockMarksKey.getState(editor.state)
  if (!set) return []
  const fresh: number[] = []
  let index = 0
  editor.state.doc.forEach((node, offset) => {
    // find() also returns what only touches the range: a neighbour's mark
    // ends where this block starts. Only the mark on this block counts.
    if (set.find(offset, offset + node.nodeSize, (spec: { kind?: string }) => spec.kind === 'fresh').some((d) => d.from === offset)) fresh.push(index)
    index++
  })
  return fresh
}

/**
 * The document's title: one line of text that wraps like a heading, rather
 * than an input, which cuts a long title off at the panel's edge. It grows
 * with what it holds and with the panel's width; a line break is not a title's.
 */
function DocumentTitle({ value, readOnly, label, onChange }: { value: string; readOnly: boolean; label: string; onChange: (value: string) => void }) {
  const ref = useRef<HTMLTextAreaElement>(null)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const fit = () => {
      el.style.height = 'auto'
      el.style.height = `${el.scrollHeight}px`
    }
    fit()
    const observer = new ResizeObserver(fit)
    observer.observe(el)
    return () => observer.disconnect()
  }, [value])
  return (
    <textarea
      ref={ref}
      className="n-doc-title"
      rows={1}
      value={value}
      readOnly={readOnly}
      maxLength={200}
      aria-label={label}
      placeholder={label}
      onKeyDown={(event) => {
        if (event.key === 'Enter') event.preventDefault()
      }}
      onChange={(event) => onChange(event.target.value.replace(/\s*\n\s*/g, ' '))}
    />
  )
}

export default DocumentPanel
