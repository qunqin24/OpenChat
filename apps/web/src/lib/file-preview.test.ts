import { describe, expect, it } from 'vitest'

import { csvCells, pagePreview } from './file-preview'

describe('pagePreview', () => {
  it('shows a document as a titled page of plain lines', () => {
    expect(pagePreview('# 年度报告\n\n营收增长 12%\n\n- 第一点\n| a | b |\n| --- | --- |\n| 1 | 2 |\n', false)).toEqual({
      kind: 'page',
      title: '年度报告',
      lines: ['营收增长 12%', '第一点', 'a  b', '1  2'],
    })
  })

  it('drops Markdown emphasis', () => {
    expect(pagePreview('## **中国当代青年**\n**人口结构**与 `代码`', false)).toEqual({ kind: 'page', title: '中国当代青年', lines: ['人口结构与 代码'] })
  })

  it('leaves out the marks the server adds', () => {
    expect(pagePreview('[Page 1]\nAbstract\n\n[Page 2]\nMore', false)).toEqual({ kind: 'page', lines: ['Abstract', 'More'] })
    expect(pagePreview('## Sheet: 销售\n\n月份,"金额, 元"\n一月,1200\n', true)).toEqual({
      kind: 'table',
      rows: [['月份', '金额, 元'], ['一月', '1200']],
    })
    expect(pagePreview('[Page 1]\n\n', false)).toEqual({ kind: 'empty' })
  })

  it('reads quoted CSV cells', () => {
    expect(csvCells('a,"b ""c""",d')).toEqual(['a', 'b "c"', 'd'])
  })
})
