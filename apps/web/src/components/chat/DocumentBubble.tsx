import type { Editor } from '@tiptap/react'
import { BubbleMenu } from '@tiptap/react/menus'
import { Bold, Check, ChevronDown, Italic, Link2, MessageSquareQuote } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

type Block = 'paragraph' | 'h1' | 'h2' | 'h3' | 'bullet' | 'ordered' | 'quote'

const BLOCKS: Block[] = ['paragraph', 'h1', 'h2', 'h3', 'bullet', 'ordered', 'quote']

function blockOf(editor: Editor): Block {
  if (editor.isActive('heading', { level: 1 })) return 'h1'
  if (editor.isActive('heading', { level: 2 })) return 'h2'
  if (editor.isActive('heading', { level: 3 })) return 'h3'
  if (editor.isActive('bulletList')) return 'bullet'
  if (editor.isActive('orderedList')) return 'ordered'
  if (editor.isActive('blockquote')) return 'quote'
  return 'paragraph'
}

function setBlock(editor: Editor, block: Block) {
  const chain = editor.chain().focus()
  // Out of whatever list or quote it is in first, so each choice is one step.
  if (editor.isActive('bulletList') && block !== 'bullet') chain.toggleBulletList()
  if (editor.isActive('orderedList') && block !== 'ordered') chain.toggleOrderedList()
  if (editor.isActive('blockquote') && block !== 'quote') chain.toggleBlockquote()
  switch (block) {
    case 'paragraph': chain.setParagraph(); break
    case 'h1': chain.setHeading({ level: 1 }); break
    case 'h2': chain.setHeading({ level: 2 }); break
    case 'h3': chain.setHeading({ level: 3 }); break
    case 'bullet': if (!editor.isActive('bulletList')) chain.setParagraph().toggleBulletList(); break
    case 'ordered': if (!editor.isActive('orderedList')) chain.setParagraph().toggleOrderedList(); break
    case 'quote': if (!editor.isActive('blockquote')) chain.setParagraph().toggleBlockquote(); break
  }
  chain.run()
}

/**
 * The bar over a selection in a writing block, as ChatGPT's: ask the model to
 * change just this (the passage goes to the composer as a quote), a link,
 * bold, italic and the paragraph's style. Shown only while the text is
 * editable - not over an older version.
 *
 * Everything stays inside the bar, the style list included, and no button
 * takes the focus from the editor (mouse down is cancelled): the bar hides
 * when the editor loses focus, and a menu in a portal outside it lost its
 * anchor and opened in the page's corner.
 */
export function DocumentBubble({ editor, onAsk, canAsk }: { editor: Editor; onAsk: () => void; canAsk: boolean }) {
  const { t } = useTranslation()
  const [linking, setLinking] = useState(false)
  const [href, setHref] = useState('')
  const [styling, setStyling] = useState(false)
  const block = blockOf(editor)
  // Keeps the editor's focus, and so the selection and this bar, on a click.
  const keep = (event: React.MouseEvent) => event.preventDefault()

  const applyLink = () => {
    const url = href.trim()
    const chain = editor.chain().focus().extendMarkRange('link')
    if (!url) chain.unsetLink().run()
    else chain.setLink({ href: /^[a-z]+:/i.test(url) ? url : `https://${url}` }).run()
    setLinking(false)
  }

  return (
    <BubbleMenu
      editor={editor}
      className="n-doc-bubble"
      options={{ placement: 'top-start', offset: 8 }}
      shouldShow={({ editor: e, from, to }) => e.isEditable && from !== to}
      onMouseDown={(event) => {
        // The link field needs its own focus; everything else keeps the editor's.
        if (!(event.target instanceof HTMLInputElement)) keep(event)
      }}
    >
      {linking ? (
        <form className="n-doc-bubble-link" onSubmit={(event) => { event.preventDefault(); applyLink() }}>
          <input
            autoFocus
            value={href}
            placeholder={t('document.link-placeholder')}
            aria-label={t('document.link')}
            onChange={(event) => setHref(event.target.value)}
            onKeyDown={(event) => { if (event.key === 'Escape') { event.preventDefault(); setLinking(false) } }}
          />
          <button type="submit" className="n-doc-bubble-btn" aria-label={t('document.link-apply')}><Check /></button>
        </form>
      ) : (
        <>
          <button type="button" className="n-doc-bubble-ask" disabled={!canAsk} onClick={onAsk}>
            <MessageSquareQuote aria-hidden="true" />
            {t('document.ask')}
          </button>
          <span className="n-doc-bubble-sep" aria-hidden="true" />
          <button
            type="button"
            className={cn('n-doc-bubble-btn', editor.isActive('link') && 'is-on')}
            aria-label={t('document.link')}
            title={t('document.link')}
            onClick={() => {
              setHref((editor.getAttributes('link').href as string | undefined) ?? '')
              setLinking(true)
            }}
          >
            <Link2 />
          </button>
          <button type="button" className={cn('n-doc-bubble-btn', editor.isActive('bold') && 'is-on')} aria-label={t('document.bold')} aria-pressed={editor.isActive('bold')} title={t('document.bold')} onClick={() => editor.chain().focus().toggleBold().run()}>
            <Bold />
          </button>
          <button type="button" className={cn('n-doc-bubble-btn', editor.isActive('italic') && 'is-on')} aria-label={t('document.italic')} aria-pressed={editor.isActive('italic')} title={t('document.italic')} onClick={() => editor.chain().focus().toggleItalic().run()}>
            <Italic />
          </button>
          <div className="n-doc-bubble-menu">
            <button type="button" className="n-doc-bubble-style" aria-haspopup="listbox" aria-expanded={styling} onClick={() => setStyling((open) => !open)}>
              {t(`document.block.${block}`)}
              <ChevronDown aria-hidden="true" />
            </button>
            {styling && (
              <div className="n-doc-bubble-list" role="listbox" aria-label={t('document.block.label')}>
                {BLOCKS.map((value) => (
                  <button
                    key={value}
                    type="button"
                    role="option"
                    aria-selected={value === block}
                    className="n-doc-bubble-option"
                    onClick={() => {
                      setBlock(editor, value)
                      setStyling(false)
                    }}
                  >
                    <span className={cn('n-doc-block-option', `is-${value}`)}>{t(`document.block.${value}`)}</span>
                    {value === block && <Check aria-hidden="true" />}
                  </button>
                ))}
              </div>
            )}
          </div>
        </>
      )}
    </BubbleMenu>
  )
}

export default DocumentBubble
