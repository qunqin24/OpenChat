import type { AvailableSkill, ConversationReference } from '@openchat/api-client'
import { ArrowUp, BookOpen, Maximize2, MessageSquareQuote, Minimize2, NotebookPen, Square, X } from 'lucide-react'
import { useEffect, useRef, useState, type ClipboardEvent, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { mentionAt } from '@openchat/chat-core/skill-mention'

import type { PendingAttachment } from '@/hooks/use-attachments'
import { highlightPassage } from '@/lib/highlight-passage'
import { isEnter, withCtrl, withShift } from '@/lib/keys'
import { cn } from '@/lib/utils'
import { AttachMenu } from './AttachMenu'
import AttachmentTray from './AttachmentTray'
import Tooltip, { TooltipGroup } from './Tooltip'

interface ComposerProps {
  value: string
  onValueChange: (value: string) => void
  onSend: () => void
  onStop: () => void
  working: boolean
  disabled?: boolean
  /** The model picker, docked beside the send button. */
  modelSlot?: ReactNode
  /** Switches for this conversation's tools, beside the attach button. */
  toolsSlot?: ReactNode
  /**
   * A quiet line above the box about what sending from it does: set while the
   * box is the editor of a message content moderation refused, where sending
   * rewords that message instead of adding a new one.
   */
  note?: string
  /**
   * File attachments. `accept` is the picker's filter and is empty when the
   * model takes no files, which leaves the button off; `hint` says what the
   * button does, or why it does not.
   */
  /**
   * Passages quoted for the request being written: one of a writing block
   * ("提出修改请求"), or several of answers ("引用提问"). Shown above the box
   * as cards in a row, each with its own remove.
   */
  quotes?: { title: string; passage: string; fromAnswer?: boolean; onRemove: () => void }[]
  /**
   * Skills picked for the message with `$`: typing `$` at the start of the box
   * or after a space lists what this conversation's model can open; the ones
   * picked are chips above the box, sent as lines at the message's start
   * (chat-core/src/skill-mention.ts).
   */
  skills?: {
    picked: string[]
    available: AvailableSkill[]
    onPick: (name: string) => void
    onUnpick: (name: string) => void
  }
  attach?: {
    accept: string
    hint: string
    items: PendingAttachment[]
    onAdd: (files: File[]) => void
    onRemove: (key: string) => void
    /** The conversation written in, which the "+" menu does not offer to refer to. */
    conversationId?: string
    /** The other conversations the message is to refer to, and the ways to change them. */
    references: ConversationReference[]
    onRefer: (reference: ConversationReference) => void
    onUnrefer: (conversationId: string) => void
    /** Why sending is held back, when a file is still going up or failed. */
    blocked?: string
  }
}

export function Composer({
  value,
  onValueChange,
  onSend,
  onStop,
  working,
  disabled,
  modelSlot,
  toolsSlot,
  note,
  quotes,
  skills,
  attach,
}: ComposerProps) {
  const { t } = useTranslation()
  // Where the caret is, for the $ picker; and the `$` the person closed it on.
  const [caret, setCaret] = useState(0)
  const [dismissed, setDismissed] = useState<number | null>(null)
  const [active, setActive] = useState(0)
  const mention = skills ? mentionAt(value, caret) : null
  const matches = mention
    ? (skills?.available ?? [])
        .filter((s) => !skills?.picked.includes(s.name))
        .filter((s) => s.name.includes(mention.query) || s.description.toLowerCase().includes(mention.query))
        .slice(0, 8)
    : []
  const picking = Boolean(mention) && matches.length > 0 && dismissed !== mention?.start
  const pick = (name: string) => {
    if (!mention || !skills) return
    const next = value.slice(0, mention.start) + value.slice(caret)
    onValueChange(next)
    skills.onPick(name)
    setActive(0)
    requestAnimationFrame(() => {
      ref.current?.focus()
      ref.current?.setSelectionRange(mention.start, mention.start)
      setCaret(mention.start)
    })
  }
  // A passage of an answer shown where it is (a click on its card): its
  // highlight goes when it is clicked again, removed or sent.
  const [picked, setPicked] = useState<string | null>(null)
  const quoteKey = (quotes ?? []).map((q) => q.passage).join('\u0000')
  // Shown only while its card is still there.
  const shownQuote = picked && quoteKey.split('\u0000').includes(picked) ? picked : null
  const showQuote = (passage: string) => {
    const next = shownQuote === passage ? null : passage
    setPicked(next && highlightPassage(next) ? next : null)
    if (!next) highlightPassage(null)
  }
  // Its card removed, or the message sent: the highlight goes with it.
  useEffect(() => {
    if (picked && !shownQuote) highlightPassage(null)
  }, [picked, shownQuote])
  useEffect(() => () => void highlightPassage(null), [])
  // What the box asks for: a change to a writing block's passage, a question
  // about passages of answers, or a message.
  const first = quotes?.[0]
  const placeholder = first
    ? t(first.fromAnswer ? 'chat.quote-placeholder' : 'document.quote-placeholder')
    : t('chat.placeholder')
  const ref = useRef<HTMLTextAreaElement>(null)
  const picker = useRef<HTMLInputElement>(null)

  const [expanded, setExpanded] = useState(false)
  const [overflowing, setOverflowing] = useState(false)

  // Grow with the content up to the CSS max-height, then scroll.
  useEffect(() => {
    const el = ref.current
    if (!el) return

    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`

    // `clientHeight` is the height after the CSS clamp, so this asks the browser
    // whether the text actually overflows rather than duplicating the max-height
    // as a magic number here. Skipped while expanded, otherwise the toggle would
    // vanish the moment the taller box fits the text.
    if (!expanded) setOverflowing(el.scrollHeight > el.clientHeight)
  }, [value, expanded])

  const canUpload = !!attach?.accept
  // Not while an answer is being made: Enter must not queue a second message behind it.
  const canSend = value.trim().length > 0 && !disabled && !working && !attach?.blocked

  // Images copied from elsewhere paste in as files.
  const handlePaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(event.clipboardData.files)
    if (files.length === 0 || !attach) return
    event.preventDefault()
    attach.onAdd(files)
  }
  const showToggle = expanded || overflowing

  return (
    // One group for the whole composer: hovering along the button row swaps
    // tooltips instantly instead of re-paying the open delay per button.
    <TooltipGroup>
      {note && (
        <p className="n-composer-note" role="status">
          {note}
        </p>
      )}
      <div className={cn('n-composer', expanded && 'expanded', showToggle && 'has-toggle')}>
        {showToggle && (
          <Tooltip
            label={expanded ? t('chat.composer.collapse') : t('chat.composer.expand')}
            side="left"
          >
            <button
              type="button"
              className="n-composer-toggle n-icon-btn sm"
              aria-expanded={expanded}
              aria-label={expanded ? t('chat.composer.collapse') : t('chat.composer.expand')}
              onClick={() => setExpanded(!expanded)}
            >
              {expanded ? (
                <Minimize2 className="h-3.5 w-3.5" />
              ) : (
                <Maximize2 className="h-3.5 w-3.5" />
              )}
            </button>
          </Tooltip>
        )}

        {quotes && quotes.length > 0 && (
          <div className="n-quote-tray">
            {quotes.map((quote, i) => (
              <div
                key={`${i}-${quote.passage.slice(0, 24)}`}
                className={cn('n-quote-card', quote.fromAnswer && 'is-showable', shownQuote === quote.passage && 'is-shown')}
                {...(quote.fromAnswer
                  ? {
                      role: 'button',
                      tabIndex: 0,
                      'aria-pressed': shownQuote === quote.passage,
                      'aria-label': t('chat.quote-show'),
                      onClick: () => showQuote(quote.passage),
                      onKeyDown: (event: ReactKeyboardEvent) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault()
                          showQuote(quote.passage)
                        }
                      },
                    }
                  : {})}
              >
                <p className="n-quote-text" dir="auto">{quote.passage}</p>
                <span className="n-quote-label">
                  {quote.fromAnswer ? <MessageSquareQuote aria-hidden="true" /> : <NotebookPen aria-hidden="true" />}
                  {quote.fromAnswer
                    ? quotes.length > 1
                      ? t('chat.quote-label-n', { n: i + 1 })
                      : t('chat.quote-label')
                    : quote.title || t('document.quote-label')}
                </span>
                <button
                  type="button"
                  className="n-quote-remove"
                  aria-label={t(quote.fromAnswer ? 'chat.quote-remove' : 'document.quote-remove')}
                  onClick={(event) => {
                    event.stopPropagation()
                    quote.onRemove()
                  }}
                >
                  <X aria-hidden="true" />
                </button>
              </div>
            ))}
          </div>
        )}
        {skills && skills.picked.length > 0 && (
          <div className="n-skill-chips">
            {skills.picked.map((name) => (
              <span key={name} className="n-skill-chip">
                <BookOpen aria-hidden="true" />
                <span>{name}</span>
                <button type="button" aria-label={t('chat.skills.remove', { name })} onClick={() => skills.onUnpick(name)}>
                  <X aria-hidden="true" />
                </button>
              </span>
            ))}
          </div>
        )}
        {attach && (
          <AttachmentTray
            items={attach.items}
            onRemove={attach.onRemove}
            references={attach.references}
            onRemoveReference={attach.onUnrefer}
          />
        )}

        <div className="n-composer-field">
          {picking && (
            <ul className="n-skill-picker" role="listbox" aria-label={t('chat.skills.picker')}>
              {matches.map((skill, i) => (
                <li
                  key={skill.name}
                  role="option"
                  aria-selected={i === Math.min(active, matches.length - 1)}
                  className={cn('n-skill-option', i === Math.min(active, matches.length - 1) && 'is-active')}
                  // Before the box loses focus, so the caret is still known.
                  onMouseDown={(event) => {
                    event.preventDefault()
                    pick(skill.name)
                  }}
                  onMouseEnter={() => setActive(i)}
                >
                  <span className="n-skill-option-name">
                    <BookOpen aria-hidden="true" />
                    {skill.name}
                    {skill.source !== 'personal' && (
                      <span className="n-skill-option-source">{t(skill.source === 'builtin' ? 'chat.skills.source-builtin' : 'chat.skills.source-site')}</span>
                    )}
                  </span>
                  <span className="n-skill-option-description">{skill.description}</span>
                </li>
              ))}
            </ul>
          )}
          <textarea
            id="input"
            ref={ref}
            className="n-composer-input"
            rows={1}
            value={value}
            disabled={disabled}
            aria-label={placeholder}
            placeholder={placeholder}
            onChange={(event) => {
              onValueChange(event.target.value)
              setCaret(event.target.selectionStart)
              setActive(0)
            }}
            onSelect={(event) => setCaret(event.currentTarget.selectionStart)}
            onPaste={handlePaste}
            onKeyDown={(event) => {
              if (picking && mention && !event.nativeEvent.isComposing) {
                const last = matches.length - 1
                if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                  event.preventDefault()
                  const at = Math.min(active, last)
                  setActive(event.key === 'ArrowDown' ? (at + 1) % matches.length : (at - 1 + matches.length) % matches.length)
                  return
                }
                if (event.key === 'Enter' || event.key === 'Tab') {
                  event.preventDefault()
                  pick(matches[Math.min(active, last)]!.name)
                  return
                }
                if (event.key === 'Escape') {
                  event.preventDefault()
                  setDismissed(mention.start)
                  return
                }
              }
              if (!isEnter(event)) return
              // Bare Enter sends; Ctrl/Cmd or Shift inserts a newline.
              if (withCtrl(event) || withShift(event)) return
              event.preventDefault()
              if (canSend) onSend()
            }}
          />
        </div>

        <div className="n-composer-bar">
          <AttachMenu
            disabled={!canUpload}
            hint={attach?.hint ?? t('chat.file.none')}
            conversationId={attach?.conversationId}
            references={attach?.references ?? []}
            onPickFiles={() => picker.current?.click()}
            onAdd={(files) => attach?.onAdd(files)}
            onRefer={(reference) => attach?.onRefer(reference)}
          />
          {attach && (
            <input
              ref={picker}
              type="file"
              multiple
              hidden
              accept={attach.accept}
              onChange={(event) => {
                attach.onAdd(Array.from(event.target.files ?? []))
                // Picking the same file again must still fire a change.
                event.target.value = ''
              }}
            />
          )}

          {toolsSlot}

          <div className="grow" />

          {modelSlot}

          <Tooltip label={working ? t('chat.stop') : (attach?.blocked ?? t('chat.send'))}>
            <span className="n-tip-wrap">
              <button
                type="button"
                className="n-icon-btn primary"
                disabled={!working && !canSend}
                aria-label={working ? t('chat.stop') : t('chat.send')}
                onClick={() => (working ? onStop() : onSend())}
              >
                {working ? (
                  <Square className="h-3.5 w-3.5 fill-current" />
                ) : (
                  <ArrowUp className="h-[18px] w-[18px]" />
                )}
              </button>
            </span>
          </Tooltip>
        </div>
      </div>
    </TooltipGroup>
  )
}

export default Composer
