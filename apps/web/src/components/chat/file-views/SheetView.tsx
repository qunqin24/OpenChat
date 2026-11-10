import { useEffect, useLayoutEffect, useMemo, useReducer, useRef, useState, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'
import { RowHeights } from '@openchat/chat-core/row-heights'
import { columnName, columnWidth, columnWidths, rowLines, widestCells, type Sheet } from '@openchat/chat-core/workbook'

/** A row of one line, and the column letters' row: document.css draws them this tall. */
const ROW_HEIGHT = 32
/** A line of a cell, and what a row is taller than its lines by (padding and rule). */
const LINE_HEIGHT = 20
const ROW_EXTRA = ROW_HEIGHT - LINE_HEIGHT
/** A cell wraps onto this many lines at most; the rest is cut, the whole of it in its tooltip. */
const MAX_LINES = 12
/** Rows drawn beyond the view on either side, for a scroll to land on. */
const OVERSCAN = 12
/** The column of row numbers. */
const NUMBERS_WIDTH = 48

/**
 * A table in the file panel: an Excel file's sheets as tabs (parseWorkbook,
 * from the words the server reads out of it - the values Excel shows, not its
 * fonts, colours or merged cells), or a CSV or TSV file (parseDelimited);
 * each a grid with the columns lettered and the rows numbered.
 *
 * Only the rows in view are drawn, in columns whose widths are worked out once
 * from the cells: a sheet of thousands of rows drawn whole made every frame of
 * dragging the panel wider lay out tens of thousands of cells. A cell longer
 * than its column wraps, so a row is as tall as its tallest cell.
 */
export function SheetView({ sheets }: { sheets: Sheet[] }) {
  const { t } = useTranslation()
  const [index, setIndex] = useState(0)
  const sheet = sheets[Math.min(index, sheets.length - 1)]

  if (!sheet) return <p className="n-file-panel-note">{t('chat.file.sheet-empty')}</p>
  return (
    <div className="n-sheet-view">
      {sheets.length > 1 && (
        <div className="n-sheet-tabs" role="tablist" aria-label={t('chat.file.sheets')}>
          {sheets.map((each, i) => (
            <button
              key={i}
              type="button"
              role="tab"
              aria-selected={each === sheet}
              className={cn('n-sheet-tab', each === sheet && 'active')}
              onClick={() => setIndex(i)}
              dir="auto"
            >
              {each.name}
            </button>
          ))}
        </div>
      )}
      {sheet.rows.length === 0 ? (
        <p className="n-file-panel-note">{t('chat.file.sheet-empty')}</p>
      ) : (
        <Grid key={index} rows={sheet.rows} />
      )}
    </div>
  )
}

function Grid({ rows }: { rows: string[][] }) {
  const scroll = useRef<HTMLDivElement>(null)
  const probe = useRef<HTMLSpanElement>(null)
  const body = useRef<HTMLTableSectionElement>(null)
  const [top, setTop] = useState(0)
  const [height, setHeight] = useState(800)
  const [, redraw] = useReducer((n: number) => n + 1, 0)

  // The columns' widths: estimated from the characters for the first draw,
  // then from the likely widest cells of each column measured in the grid's
  // own type (before that draw is painted). An estimate that is a few pixels
  // short would break a number in two.
  const estimated = useMemo(() => columnWidths(rows), [rows])
  const [sized, setSized] = useState<{ rows: string[][]; widths: number[] } | null>(null)
  const widths = sized?.rows === rows ? sized.widths : estimated
  useLayoutEffect(() => {
    const span = probe.current
    if (!span) return
    let laidOut = true
    const measured = widestCells(rows).map((cells) => {
      let widest = 0
      for (const cell of cells) {
        span.textContent = cell
        const width = span.getBoundingClientRect().width
        if (width === 0) laidOut = false
        widest = Math.max(widest, width)
      }
      return columnWidth(widest)
    })
    span.textContent = ''
    // Not laid out (the panel is hidden): the estimate stands.
    if (laidOut) setSized({ rows, widths: measured })
  }, [rows])

  // The table is given its width in pixels, the sum of its columns. A table
  // whose width is `auto` or `max-content` is laid out in auto mode whatever
  // `table-layout` says: the columns then grow to the longest cell among the
  // rows drawn, and change width as rows scroll in and out of view.
  const width = useMemo(() => widths.reduce((sum, each) => sum + each, NUMBERS_WIDTH), [widths])

  // How tall each row is: estimated from its cells until it has been drawn,
  // then what it measured (below).
  const heights = useMemo(
    () => new RowHeights(rowLines(rows, widths, MAX_LINES).map((lines) => lines * LINE_HEIGHT + ROW_EXTRA)),
    [rows, widths],
  )

  useEffect(() => {
    const element = scroll.current
    if (!element) return
    const measure = () => setHeight(element.clientHeight)
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  // The rows begin under the column letters.
  const viewTop = Math.max(0, top - ROW_HEIGHT)
  const first = Math.max(0, heights.at(viewTop) - OVERSCAN)
  const last = Math.min(rows.length, heights.at(viewTop + height) + 1 + OVERSCAN)
  const span = widths.length + 1

  // After every draw: the rows drawn are measured, and where one is not the
  // height it was taken for, the rows are placed again. Rows above the view
  // that turn out taller or shorter would move what is on screen, so the view
  // is scrolled by the same amount, before anything is painted.
  useLayoutEffect(() => {
    const element = scroll.current
    const section = body.current
    if (!element || !section) return
    const drawn = Array.from(section.querySelectorAll<HTMLTableRowElement>('tr[data-row]'), (row) => row.getBoundingClientRect().height)
    const { changed, shift } = heights.measure(first, drawn, Math.max(0, element.scrollTop - ROW_HEIGHT))
    if (!changed) return
    if (shift !== 0) element.scrollTop += shift
    redraw()
  })

  return (
    <div ref={scroll} className="n-sheet-scroll" onScroll={(event) => setTop(event.currentTarget.scrollTop)}>
      <span ref={probe} className="n-sheet-probe" aria-hidden="true" />
      <table className="n-sheet-grid" style={{ width, '--n-sheet-lines': MAX_LINES } as CSSProperties}>
        <colgroup>
          <col style={{ width: NUMBERS_WIDTH }} />
          {widths.map((each, i) => (
            <col key={i} style={{ width: each }} />
          ))}
        </colgroup>
        <thead>
          <tr>
            <th aria-hidden="true" />
            {widths.map((_, i) => (
              <th key={i} scope="col">{columnName(i)}</th>
            ))}
          </tr>
        </thead>
        <tbody ref={body}>
          {first > 0 && (
            <tr aria-hidden="true" style={{ height: heights.start(first) }}>
              <td colSpan={span} className="n-sheet-gap" />
            </tr>
          )}
          {rows.slice(first, last).map((row, i) => (
            <tr key={first + i} data-row="">
              <th scope="row">{first + i + 1}</th>
              {widths.map((_, c) => (
                <td key={c} dir="auto" title={row[c] && row[c].length > 30 ? row[c] : undefined}>
                  {row[c] ? <div className="n-sheet-cell">{row[c]}</div> : null}
                </td>
              ))}
            </tr>
          ))}
          {last < rows.length && (
            <tr aria-hidden="true" style={{ height: heights.total - heights.start(last) }}>
              <td colSpan={span} className="n-sheet-gap" />
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}

export default SheetView
