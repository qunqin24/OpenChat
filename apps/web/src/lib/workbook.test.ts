import { describe, expect, it } from 'vitest'

import { columnName, columnWidths, parseDelimited, parseWorkbook } from './workbook'

describe('parseWorkbook', () => {
  it('reads the server text back into sheets', () => {
    const text = '## Sheet: 销售\n\n月份,"金额, 元"\n一月,"第一行\n第二行"\n\n## Sheet: Empty\n\n(empty)\n\n## Sheet: Notes\n\n"say ""hi"""\n'
    expect(parseWorkbook(text)).toEqual([
      { name: '销售', rows: [['月份', '金额, 元'], ['一月', '第一行\n第二行']] },
      { name: 'Empty', rows: [] },
      { name: 'Notes', rows: [['say "hi"']] },
    ])
  })

  it('takes a heading off its own line, never a cell for one', () => {
    const text =
      '## Sheet: Sales, 2026\n\n"## Sheet: Important",x\n\na\n\n## Sheet: Odd\n\n"(empty)"\n\n## Sheet: None\n\n(empty)\n'
    expect(parseWorkbook(text)).toEqual([
      { name: 'Sales, 2026', rows: [['## Sheet: Important', 'x'], [], ['a']] },
      { name: 'Odd', rows: [['(empty)']] },
      { name: 'None', rows: [] },
    ])
  })

  it('reads a CSV or TSV file as one sheet', () => {
    expect(parseDelimited('a,b\r\n1,"x, y"\n\n', 'data.csv', ',')).toEqual({ name: 'data.csv', rows: [['a', 'b'], ['1', 'x, y']] })
    expect(parseDelimited('a\tb\n1\t2', 'data.tsv', '\t').rows).toEqual([['a', 'b'], ['1', '2']])
  })

  it('sizes columns by their longest cell, within bounds', () => {
    expect(columnWidths([['a', '营收（万元）', 'x'.repeat(100)], ['abcdefghij']])).toEqual([97, 112, 280])
  })

  it('counts every column, even one that starts past the rows it samples', () => {
    const rows = [...Array.from({ length: 500 }, () => ['a']), ['a', 'b', 'c']]
    expect(columnWidths(rows)).toHaveLength(3)
  })

  it('names columns as a spreadsheet does', () => {
    expect([0, 25, 26, 27, 701, 702].map(columnName)).toEqual(['A', 'Z', 'AA', 'AB', 'ZZ', 'AAA'])
  })
})
