import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'
import { columnName, columnWidths, type Sheet } from '@/lib/workbook'

/** Every row is this tall, so only the rows in view need drawing. */
const ROW_HEIGHT = 32
/** Rows drawn beyond the view on either side, for a scroll to land on. */
const OVERSCAN = 12

/**
 * A table in the file panel: an Excel file's sheets as tabs (parseWorkbook,
 * from the words the server reads out of it - the values Excel shows, not its
 * fonts, colours or merged cells), or a CSV or TSV file (parseDelimited);
 * each a grid with the columns lettered and the rows numbered.
 *
 * Only the rows in view are drawn, at fixed heights and column widths worked
 * out once from the cells: a sheet of thousands of rows drawn whole made every
 * frame of dragging the panel wider lay out tens of thousands of cells.
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
  const [top, setTop] = useState(0)
  const [height, setHeight] = useState(800)
  const widths = useMemo(() => columnWidths(rows), [rows])

  useEffect(() => {
    const element = scroll.current
    if (!element) return
    const measure = () => setHeight(element.clientHeight)
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  const first = Math.max(0, Math.floor(top / ROW_HEIGHT) - OVERSCAN)
  const last = Math.min(rows.length, Math.ceil((top + height) / ROW_HEIGHT) + OVERSCAN)
  const span = widths.length + 1

  return (
    <div ref={scroll} className="n-sheet-scroll" onScroll={(event) => setTop(event.currentTarget.scrollTop)}>
      <table className="n-sheet-grid">
        <colgroup>
          <col style={{ width: 48 }} />
          {widths.map((width, i) => (
            <col key={i} style={{ width }} />
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
        <tbody>
          {first > 0 && (
            <tr aria-hidden="true" style={{ height: first * ROW_HEIGHT }}>
              <td colSpan={span} className="n-sheet-gap" />
            </tr>
          )}
          {rows.slice(first, last).map((row, i) => (
            <tr key={first + i}>
              <th scope="row">{first + i + 1}</th>
              {widths.map((_, c) => (
                <td key={c} dir="auto" title={row[c] && row[c].length > 30 ? row[c] : undefined}>{row[c] ?? ''}</td>
              ))}
            </tr>
          ))}
          {last < rows.length && (
            <tr aria-hidden="true" style={{ height: (rows.length - last) * ROW_HEIGHT }}>
              <td colSpan={span} className="n-sheet-gap" />
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}

export default SheetView
