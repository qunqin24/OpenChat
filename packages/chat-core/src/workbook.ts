/**
 * A workbook as the server reads it out of an Excel file (internal/doctext):
 * each sheet a "## Sheet: name" line and a blank one, then its rows as CSV and
 * a blank line, or "(empty)" for a sheet with none. Read back here into sheets
 * of rows for the file panel's grid (SheetView); a quoted cell may hold
 * commas, quotes and line breaks.
 *
 * The heading and "(empty)" are read off the raw line where a record starts,
 * never out of the CSV: a sheet's name is the rest of that line whatever it
 * holds ("Sales, 2026"), and a cell that reads like either is quoted by the
 * server, so it starts with a quote and is a cell.
 */
export interface Sheet {
  name: string
  rows: string[][]
}

const SHEET = '## Sheet: '
const EMPTY = '(empty)'

export function parseWorkbook(text: string): Sheet[] {
  const sheets: Sheet[] = []
  let at = 0
  while (at < text.length) {
    const lineEnd = text.indexOf('\n', at) === -1 ? text.length : text.indexOf('\n', at)
    const line = text.slice(at, lineEnd).replace(/\r$/, '')
    if (line.startsWith(SHEET)) {
      sheets.push({ name: line.slice(SHEET.length), rows: [] })
      at = lineEnd + 1
      // The blank line under the heading, and the word for a sheet with no rows.
      if (text[at] === '\n') at++
      const next = text.indexOf('\n', at) === -1 ? text.length : text.indexOf('\n', at)
      if (text.slice(at, next).replace(/\r$/, '') === EMPTY) at = next + 1
      continue
    }
    const [record, end] = csvRecord(text, at)
    at = end
    const sheet = sheets[sheets.length - 1]
    if (!sheet) continue
    // The blank line that ends a sheet's rows; a blank row inside them stays,
    // so the rows keep the numbers Excel gives them.
    const blank = record.length === 1 && record[0] === ''
    if (blank && (at >= text.length || text.startsWith(SHEET, at))) continue
    sheet.rows.push(blank ? [] : record)
  }
  return sheets
}

/** The separator of a text file that is a table, by its name: CSV or TSV; null for any other. */
export function delimiterOf(filename: string): ',' | '\t' | null {
  const name = filename.toLowerCase()
  return name.endsWith('.csv') ? ',' : name.endsWith('.tsv') ? '\t' : null
}

/** A CSV or TSV file as one sheet of rows; blank lines are left out. */
export function parseDelimited(text: string, name: string, separator: ',' | '\t'): Sheet {
  const rows: string[][] = []
  for (let at = 0; at < text.length; ) {
    const [record, end] = csvRecord(text, at, separator)
    at = end
    if (record.length > 1 || record[0] !== '') rows.push(record)
  }
  return { name, rows }
}

/**
 * The CSV record that starts at `from`, quoted cells understood across lines,
 * and where the next one starts.
 */
function csvRecord(text: string, from: number, separator: ',' | '\t' = ','): [string[], number] {
  const record: string[] = []
  let cell = ''
  let quoted = false
  for (let i = from; i < text.length; i++) {
    const c = text[i]
    if (quoted) {
      if (c === '"' && text[i + 1] === '"') {
        cell += '"'
        i++
      } else if (c === '"') quoted = false
      else cell += c
    } else if (c === '"' && cell === '') quoted = true
    else if (c === separator) {
      record.push(cell)
      cell = ''
    } else if (c === '\n') {
      record.push(cell.replace(/\r$/, ''))
      return [record, i + 1]
    } else cell += c
  }
  record.push(cell)
  return [record, text.length]
}

/** A column's letters as a spreadsheet names it: A … Z, AA … */
export function columnName(index: number): string {
  let name = ''
  for (let n = index + 1; n > 0; n = Math.floor((n - 1) / 26)) name = String.fromCharCode(65 + ((n - 1) % 26)) + name
  return name
}

/** What a cell's sides take of its column: the padding and the rule. */
const CELL_SIDES = 22
/** A character's width in the grid's type, as an estimate; a wide one counts twice. */
const UNIT = 7.5
const NARROWEST = 56
const WIDEST = 280

/** A line's length in characters, a wide one - as in Chinese - counting twice. */
function units(line: string): number {
  let count = 0
  for (const char of line) count += char.codePointAt(0)! > 0x2e80 ? 2 : 1
  return count
}

/** The length of a cell's longest line: what decides how wide it is. */
function longestLine(cell: string): number {
  if (!cell.includes('\n')) return units(cell)
  let longest = 0
  for (const line of cell.split('\n')) longest = Math.max(longest, units(line))
  return longest
}

/** A column's width for text this many pixels wide, between a narrow and a wide bound. */
export function columnWidth(textWidth: number): number {
  return Math.min(WIDEST, Math.max(NARROWEST, Math.ceil(textWidth) + CELL_SIDES))
}

/**
 * Each column's width in pixels for a grid of these rows: one for every column
 * any row has, sized from the longest cell among the first rows - a wide
 * character, as in Chinese, counting twice - between a narrow and a wide
 * bound. Worked out once, so a grid that draws only the rows in view keeps its
 * columns still as it scrolls. An estimate from the characters: a grid that can
 * measure text sizes its columns from widestCells and columnWidth instead.
 */
export function columnWidths(rows: string[][], sampled = 500): number[] {
  let columns = 0
  for (const row of rows) columns = Math.max(columns, row.length)
  const longest: number[] = Array.from({ length: columns }, () => 0)
  for (const row of rows.slice(0, sampled)) {
    row.forEach((cell, i) => {
      longest[i] = Math.max(longest[i] ?? 0, longestLine(cell))
    })
  }
  return longest.map((count) => Math.min(WIDEST, Math.max(NARROWEST, count * UNIT + CELL_SIDES)))
}

/**
 * For every column, the few cells among the first rows that are likely the
 * widest (the longest by their characters): what a grid measures to size the
 * column by the type it is really drawn in, where a digit or a capital is not
 * the width the estimate takes it for. A cell of several lines is given as its
 * longest line.
 */
export function widestCells(rows: string[][], sampled = 500, each = 3): string[][] {
  let columns = 0
  for (const row of rows) columns = Math.max(columns, row.length)
  const kept: { text: string; count: number }[][] = Array.from({ length: columns }, () => [])
  for (const row of rows.slice(0, sampled)) {
    row.forEach((cell, i) => {
      if (!cell) return
      const text = cell.includes('\n') ? cell.split('\n').reduce((longest, line) => (units(line) > units(longest) ? line : longest)) : cell
      if (!text) return
      const count = units(text)
      const list = kept[i]!
      if (list.length === each && count <= list[each - 1]!.count) return
      if (list.some((one) => one.text === text)) return
      list.push({ text, count })
      list.sort((a, b) => b.count - a.count)
      if (list.length > each) list.length = each
    })
  }
  return kept.map((list) => list.map((one) => one.text))
}

/**
 * How many lines each row takes when its cells wrap at their column's width,
 * at most `most` (the grid cuts a cell there). An estimate from the
 * characters, for where the rows that have not been drawn yet are taken to
 * be; the grid measures the ones it draws.
 */
export function rowLines(rows: string[][], widths: number[], most: number): number[] {
  return rows.map((row) => {
    let lines = 1
    for (let i = 0; i < row.length && lines < most; i++) {
      const cell = row[i]!
      const room = Math.max(1, (widths[i] ?? NARROWEST) - CELL_SIDES)
      // Too short to wrap whatever its characters are, and on one line.
      if (cell.length * 2 * UNIT <= room && !cell.includes('\n')) continue
      let needed = 0
      for (const line of cell.split('\n')) needed += Math.max(1, Math.ceil((units(line) * UNIT) / room))
      lines = Math.max(lines, needed)
    }
    return Math.min(most, lines)
  })
}
