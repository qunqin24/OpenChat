import { describe, expect, it } from 'vitest'

import { columnName, columnWidth, columnWidths, parseDelimited, parseWorkbook, rowLines, widestCells } from './workbook'

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

  it('sizes a column by its longest line, not by the whole of a cell of several', () => {
    expect(columnWidths([['短\n这一行最长了\n短']])).toEqual([112])
  })

  it('bounds a measured width as it does an estimated one', () => {
    expect([0, 20.2, 90, 1000].map(columnWidth)).toEqual([56, 56, 112, 280])
  })

  it('picks the likely widest cells of each column to measure', () => {
    const rows = [['a', '一二三'], ['abcdef', ''], ['abc', 'x\n最长的一行在这里'], ['abcdef', 'y'], ['ab']]
    expect(widestCells(rows)).toEqual([['abcdef', 'abc', 'ab'], ['最长的一行在这里', '一二三', 'y']])
    // Only the rows it samples, and as many as asked for.
    expect(widestCells(rows, 2, 1)).toEqual([['abcdef'], ['一二三']])
    expect(widestCells([])).toEqual([])
    // Nothing to measure in a cell of blank lines.
    expect(widestCells([['\n\n', 'a']])).toEqual([[], ['a']])
  })

  it('estimates how many lines a row takes once its cells wrap', () => {
    const widths = [97, 280]
    const rows = [
      ['a', 'short'],
      // 100 characters at 7.5 in a column with 258 of room: three lines.
      ['a', 'x'.repeat(100)],
      // Its own line breaks count, each line at least one.
      ['a', '第一行\n\n第三行'],
      // The tallest cell decides, and never more than the most.
      ['y'.repeat(40), 'z'.repeat(5000)],
      [],
    ]
    expect(rowLines(rows, widths, 12)).toEqual([1, 3, 3, 12, 1])
    expect(rowLines([['x'.repeat(100)]], [280], 2)).toEqual([2])
  })

  it('names columns as a spreadsheet does', () => {
    expect([0, 25, 26, 27, 701, 702].map(columnName)).toEqual(['A', 'Z', 'AA', 'AB', 'ZZ', 'AAA'])
  })
})
