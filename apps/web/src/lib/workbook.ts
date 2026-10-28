/**
 * A workbook as the server reads it out of an Excel file (internal/doctext):
 * each sheet a "## Sheet: name" line, then its rows as CSV. Read back here
 * into sheets of rows for the file panel's grid (SheetView); a quoted cell may
 * hold commas, quotes and line breaks.
 */
export interface Sheet {
  name: string
  rows: string[][]
}

const SHEET = '## Sheet: '

export function parseWorkbook(text: string): Sheet[] {
  const sheets: Sheet[] = []
  for (const record of csvRecords(text)) {
    if (record.length === 1 && record[0]!.startsWith(SHEET)) {
      sheets.push({ name: record[0]!.slice(SHEET.length), rows: [] })
      continue
    }
    const sheet = sheets[sheets.length - 1]
    // A blank line between a heading and its rows, and the server's word for a
    // sheet with none.
    if (!sheet || (record.length === 1 && (record[0] === '' || (record[0] === '(empty)' && sheet.rows.length === 0)))) continue
    sheet.rows.push(record)
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
  return { name, rows: csvRecords(text, separator).filter((row) => row.length > 1 || row[0] !== '') }
}

/** The records of CSV text, quoted cells understood across lines. */
function csvRecords(text: string, separator: ',' | '\t' = ','): string[][] {
  const records: string[][] = []
  let record: string[] = []
  let cell = ''
  let quoted = false
  for (let i = 0; i < text.length; i++) {
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
      records.push(record)
      record = []
      cell = ''
    } else cell += c
  }
  if (cell !== '' || record.length > 0) {
    record.push(cell)
    records.push(record)
  }
  return records
}

/** A column's letters as a spreadsheet names it: A … Z, AA … */
export function columnName(index: number): string {
  let name = ''
  for (let n = index + 1; n > 0; n = Math.floor((n - 1) / 26)) name = String.fromCharCode(65 + ((n - 1) % 26)) + name
  return name
}

/**
 * Each column's width in pixels for a grid of these rows, from the longest
 * cell among the first rows - a wide character, as in Chinese, counting
 * twice - between a narrow and a wide bound. Worked out once, so a grid that
 * draws only the rows in view keeps its columns still as it scrolls.
 */
export function columnWidths(rows: string[][], sampled = 500): number[] {
  const longest: number[] = []
  for (const row of rows.slice(0, sampled)) {
    row.forEach((cell, i) => {
      let units = 0
      for (const char of cell) units += char.codePointAt(0)! > 0x2e80 ? 2 : 1
      longest[i] = Math.max(longest[i] ?? 0, units)
    })
  }
  return longest.map((units) => Math.min(280, Math.max(56, units * 7.5 + 22)))
}
