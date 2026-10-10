import { Check, Copy } from 'lucide-react'
import { useRef, useState, type ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'
import { extractTableDataFromElement, tableDataToMarkdown } from 'streamdown'

import { copyClipboard } from '@/lib/clipboard'

/**
 * A Markdown table in an answer: one frame, which scrolls sideways when the
 * table is wide, and one copy button.
 *
 * Replaces Streamdown's own table, which nests the table in two more frames and
 * carries copy, download and full-screen controls whose menus are styled with
 * Tailwind classes this app never generates.
 */
export function MarkdownTable({ children, node: _node, ...props }: ComponentProps<'table'> & { node?: unknown }) {
  const { t } = useTranslation()
  const ref = useRef<HTMLTableElement>(null)
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    const table = ref.current
    if (!table) return
    const markdown = tableDataToMarkdown(extractTableDataFromElement(table))
    let ok: boolean
    // HTML as well as text, so pasting into a spreadsheet or a document gives a
    // table there too; plain text gets the Markdown.
    try {
      await navigator.clipboard.write([
        new ClipboardItem({
          'text/plain': new Blob([markdown], { type: 'text/plain' }),
          'text/html': new Blob([table.outerHTML], { type: 'text/html' }),
        }),
      ])
      ok = true
    } catch {
      ok = await copyClipboard(markdown)
    }
    if (!ok) return
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1600)
  }

  return (
    <div className="n-md-table">
      <div className="n-md-table-scroll">
        <table ref={ref} {...props}>
          {children}
        </table>
      </div>
      <button type="button" className="n-icon-btn sm n-md-table-copy" aria-label={t('chat.copy-table')} onClick={() => void copy()}>
        {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
    </div>
  )
}
