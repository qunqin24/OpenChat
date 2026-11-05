import type { AvailableSkill, ConversationReference } from '@openchat/api-client'
import { ArrowUp, BookOpen, Maximize2, Minimize2, Square, Telescope, X } from 'lucide-react'
import { useEffect, useLayoutEffect, useRef, useState, type ClipboardEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { mentionAt } from '@openchat/chat-core/skill-mention'

import type { PendingAttachment } from '@/hooks/use-attachments'
import { highlightPassage } from '@/lib/highlight-passage'
import { isEnter, withCtrl, withShift } from '@/lib/keys'
import { cn } from '@/lib/utils'
import { AttachMenu } from './AttachMenu'
import { ComposerQuotes } from './QuoteBlock'
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
  /** Pictures picked with 「拿来改」 for this message: thumbnails above the box, each with its remove. */
  pictures?: { items: { id: string; handle: string; src: string }[]; onRemove: (handle: string) => void }
  /**
   * Skills picked for the message: typed for with / (a list of them, filtered
   * by what follows the /). The ones picked lead the box's text, and are sent as
   * lines at the message's start (chat-core/src/skill-mention.ts).
   */
  skills?: {
    picked: string[]
    available: AvailableSkill[]
    onPick: (name: string) => void
    onUnpick: (name: string) => void
  }
  /**
   * Deep research, when it is offered ("+" → 深度研究): picked, it leads the box
   * as a token and the message asks for one (chat-core/src/skill-mention.ts).
   * `desc` is what its line says - with what is left this month - and
   * `blocked` that this month's are used up.
   */
  research?: {
    picked: boolean
    desc: string
    blocked: boolean
    onPick: () => void
    onUnpick: () => void
  }
  /**
   * A deep research plan being revised: the box writes what to change, shown
   * under the plan's title, which `onStop` gives up.
   */
  plan?: { title: string; onStop: () => void }
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
  pictures,
  skills,
  research,
  plan,
  attach,
}: ComposerProps) {
  const { t } = useTranslation()
  const box = useRef<HTMLDivElement>(null)
  const tokens = useRef<HTMLSpanElement>(null)
  // Menus open below a box in the middle of the page (a new chat), above one at its foot.
  const menuSide = (): 'top' | 'bottom' => {
    const rect = box.current?.getBoundingClientRect()
    return rect && window.innerHeight - rect.bottom > 340 ? 'bottom' : 'top'
  }
  // / at the start of the box or after a space lists the skills, filtered by
  // what follows; where the caret is, and the / the person closed the list on.
  const [caret, setCaret] = useState(0)
  const [dismissed, setDismissed] = useState<number | null>(null)
  const [active, setActive] = useState(0)
  const [below, setBelow] = useState(false)
  const mention = skills ? mentionAt(value, caret) : null
  const entries: MenuEntry[] = mention
    ? (skills?.available ?? [])
        .filter((s) => !skills?.picked.includes(s.name))
        .map((s) => ({ key: s.name, title: s.name, desc: s.description, run: () => skills?.onPick(s.name) }))
        .filter((e) => matchesQuery(e, mention.query))
    : []
  const picking = Boolean(mention) && entries.length > 0 && dismissed !== mention?.start
  const current = Math.min(active, entries.length - 1)
  const choose = (entry: MenuEntry) => {
    if (!mention) return
    // The / and what was typed after it go; the skill is picked.
    onValueChange(value.slice(0, mention.start) + value.slice(caret))
    setActive(0)
    entry.run()
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
  const placeholder = plan
    ? t('chat.research.revise-placeholder')
    : first
      ? t(first.fromAnswer ? 'chat.quote-placeholder' : 'document.quote-placeholder')
      : t('chat.placeholder')
  const ref = useRef<HTMLTextAreaElement>(null)
  const picker = useRef<HTMLInputElement>(null)

  const [expanded, setExpanded] = useState(false)
  const [overflowing, setOverflowing] = useState(false)

  // The text starts after the picked skills (and deep research) on the first
  // line, and wraps under them: indented by their width, measured as it changes.
  const tokened = Boolean(research?.picked) || Boolean(skills?.picked.length)
  const pickedKey = `${research?.picked ? 'research ' : ''}${skills?.picked.join(' ') ?? ''}`
  const [indent, setIndent] = useState(0)
  useLayoutEffect(() => {
    const el = tokens.current
    if (!el) return
    const observer = new ResizeObserver(() => setIndent(el.offsetWidth + 6))
    observer.observe(el)
    return () => observer.disconnect()
  }, [pickedKey])

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
      <div ref={box} className={cn('n-composer', expanded && 'expanded', showToggle && 'has-toggle')}>
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
          <ComposerQuotes
            quotes={quotes}
            shown={shownQuote}
            onShow={showQuote}
            onRemove={(i) => quotes[i]?.onRemove()}
          />
        )}
        {pictures && pictures.items.length > 0 && (
          <div className="n-picture-refs" aria-label={t('chat.picture.ref-label')}>
            {pictures.items.map((picture) => (
              <span key={picture.handle} className="n-picture-ref">
                <img src={picture.src} alt="" />
                <button
                  type="button"
                  className="n-picture-ref-remove"
                  aria-label={t('chat.picture.ref-remove')}
                  title={t('chat.picture.ref-remove')}
                  onClick={() => pictures.onRemove(picture.handle)}
                >
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

        {picking && (
          <ul
            className={cn('n-at-popup', below && 'is-below')}
            role="listbox"
            aria-label={t('settings.skills.title')}
          >
            {entries.map((entry, i) => (
              <li key={entry.key} className="n-at-group">
                <div
                  role="option"
                  aria-selected={i === current}
                  className={cn('n-at-option', i === current && 'is-active')}
                  // Before the box loses focus, so the caret is still known.
                  onMouseDown={(event) => {
                    event.preventDefault()
                    choose(entry)
                  }}
                  onMouseEnter={() => setActive(i)}
                >
                  <span className="n-at-icon" aria-hidden="true"><BookOpen /></span>
                  <span className="n-at-title is-skill">{entry.title}</span>
                  <span className="n-at-desc">{entry.desc}</span>
                </div>
              </li>
            ))}
          </ul>
        )}

        {plan && (
          <div className="n-plan-chip">
            <Telescope aria-hidden="true" />
            <span>{plan.title}</span>
            <button type="button" aria-label={t('chat.research.revise-stop')} title={t('chat.research.revise-stop')} onClick={plan.onStop}>
              <X aria-hidden="true" />
            </button>
          </div>
        )}

        <div className="n-composer-field">
          {/* The picked skills sit at the start of the text, as Grok's: words
              over the box's first line, the text indented past them (below). */}
          {tokened && (
            <span ref={tokens} className="n-skill-tokens">
              {research?.picked && (
                <button
                  type="button"
                  className="n-skill-token is-research"
                  title={t('chat.research.remove')}
                  aria-label={t('chat.research.remove')}
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={research.onUnpick}
                >
                  <Telescope aria-hidden="true" />
                  {t('chat.research.token')}
                </button>
              )}
              {skills?.picked.map((name) => (
                <button
                  key={name}
                  type="button"
                  className="n-skill-token"
                  title={t('chat.skills.remove', { name })}
                  aria-label={t('chat.skills.remove', { name })}
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => skills.onUnpick(name)}
                >
                  /{name}
                </button>
              ))}
            </span>
          )}
          <textarea
            id="input"
            ref={ref}
            className="n-composer-input"
            rows={1}
            value={value}
            disabled={disabled}
            aria-label={placeholder}
            placeholder={tokened ? '' : placeholder}
            style={tokened ? { textIndent: indent } : undefined}
            onChange={(event) => {
              onValueChange(event.target.value)
              setCaret(event.target.selectionStart)
              setActive(0)
              // Which way the / list would open, from where the box is now.
              setBelow(menuSide() === 'bottom')
            }}
            onSelect={(event) => setCaret(event.currentTarget.selectionStart)}
            onPaste={handlePaste}
            onKeyDown={(event) => {
              if (picking && mention && !event.nativeEvent.isComposing) {
                if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                  event.preventDefault()
                  setActive(event.key === 'ArrowDown' ? (current + 1) % entries.length : (current - 1 + entries.length) % entries.length)
                  return
                }
                if (event.key === 'Enter' || event.key === 'Tab') {
                  event.preventDefault()
                  choose(entries[current]!)
                  return
                }
                if (event.key === 'Escape') {
                  event.preventDefault()
                  setDismissed(mention.start)
                  return
                }
              }
              // Backspace at the very start takes the last token back: a skill,
              // then deep research.
              if (
                event.key === 'Backspace' &&
                tokened &&
                event.currentTarget.selectionStart === 0 &&
                event.currentTarget.selectionEnd === 0
              ) {
                event.preventDefault()
                if (skills?.picked.length) skills.onUnpick(skills.picked[skills.picked.length - 1]!)
                else research?.onUnpick()
                return
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
            filesOff={!canUpload}
            side={menuSide}
            hint={attach?.hint ?? t('chat.file.none')}
            conversationId={attach?.conversationId}
            references={attach?.references ?? []}
            onPickFiles={() => picker.current?.click()}
            onAdd={(files) => attach?.onAdd(files)}
            onRefer={(reference) => attach?.onRefer(reference)}
            research={research && !research.picked ? { desc: research.desc, blocked: research.blocked, onPick: research.onPick } : undefined}
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

/** One line of the skills typed for with /. */
interface MenuEntry {
  key: string
  title: string
  desc: string
  run: () => void
}

/** Whether a line answers what was typed after the /: in its name or its words. */
function matchesQuery(entry: MenuEntry, query: string) {
  if (!query) return true
  return entry.title.toLocaleLowerCase().includes(query) || entry.desc.toLocaleLowerCase().includes(query)
}
