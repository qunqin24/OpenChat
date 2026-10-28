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

  it('reads a CSV or TSV file as one sheet', () => {
    expect(parseDelimited('a,b\r\n1,"x, y"\n\n', 'data.csv', ',')).toEqual({ name: 'data.csv', rows: [['a', 'b'], ['1', 'x, y']] })
    expect(parseDelimited('a\tb\n1\t2', 'data.tsv', '\t').rows).toEqual([['a', 'b'], ['1', '2']])
  })

  it('sizes columns by their longest cell, within bounds', () => {
    expect(columnWidths([['a', '营收（万元）', 'x'.repeat(100)], ['abcdefghij']])).toEqual([97, 112, 280])
  })

  it('names columns as a spreadsheet does', () => {
    expect([0, 25, 26, 27, 701, 702].map(columnName)).toEqual(['A', 'Z', 'AA', 'AB', 'ZZ', 'AAA'])
  })
})
