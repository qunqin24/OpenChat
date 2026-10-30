import { isDocument, TEXT_TYPE } from '@openchat/chat-core/attachments'

/**
 * What a library card shows of a file's beginning (FilePagePreview): a page
 * of its first lines, or for a table - CSV, an Excel sheet - its first rows.
 * The words are the file's own, or for Word, Excel and PDF what the server
 * reads out of them (GET /files/{id}/text?preview=true).
 */
export type PagePreview =
  | { kind: 'page'; title?: string; lines: string[] }
  | { kind: 'table'; rows: string[][] }
  | { kind: 'empty' }

const PREVIEW_LINES = 10
const PREVIEW_ROWS = 6
const PREVIEW_COLUMNS = 4

/** The preview of a file's first words; `table` reads them as CSV. */
export function pagePreview(text: string, table: boolean): PagePreview {
  // A PDF's "[Page n]" lines and an Excel sheet's "## Sheet:" heading are
  // the server's marks, not the file's words.
  const lines = text
    .split('\n')
    .map((line) => line.replace(/\r$/, ''))
    .filter((line) => !/^\[Page \d+\]$/.test(line.trim()) && !/^## Sheet: /.test(line))
  if (table) {
    const rows = lines.filter((line) => line.trim() !== '').slice(0, PREVIEW_ROWS).map((line) => csvCells(line).slice(0, PREVIEW_COLUMNS))
    return rows.length > 0 ? { kind: 'table', rows } : { kind: 'empty' }
  }
  const words = lines.map((line) => line.trim()).filter((line) => line !== '' && !/^\|?\s*-{3,}/.test(line))
  if (words.length === 0) return { kind: 'empty' }
  const heading = /^#{1,6}\s+(.*)$/.exec(words[0]!)
  const title = heading ? plain(heading[1]!) : undefined
  const rest = (heading ? words.slice(1) : words).slice(0, PREVIEW_LINES).map(plain)
  return title ? { kind: 'page', title, lines: rest } : { kind: 'page', lines: rest }
}

/** A line of Markdown as plain words: no heading marks, list bullets or table pipes. */
function plain(line: string): string {
  return line
    .replace(/^#{1,6}\s+/, '')
    .replace(/^[-*+]\s+/, '')
    .replace(/^\|\s*|\s*\|$/g, '')
    .replace(/\s*\|\s*/g, '  ')
    .replace(/\*\*|__|`/g, '')
}

/** One CSV line's cells, quotes understood. */
export function csvCells(line: string): string[] {
  const cells: string[] = []
  let cell = ''
  let quoted = false
  for (let i = 0; i < line.length; i++) {
    const c = line[i]
    if (quoted) {
      if (c === '"' && line[i + 1] === '"') {
        cell += '"'
        i++
      } else if (c === '"') quoted = false
      else cell += c
    } else if (c === '"') quoted = true
    else if (c === ',') {
      cells.push(cell)
      cell = ''
    } else cell += c
  }
  cells.push(cell)
  return cells
}

/** Whether a file has words to preview: a text file, Word, Excel or PDF. */
export function hasPagePreview(contentType: string): boolean {
  return contentType === TEXT_TYPE || isDocument(contentType) || contentType === 'application/pdf'
}
