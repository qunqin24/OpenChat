import { describe, expect, it } from 'vitest'

import { RowHeights } from './row-heights'

describe('RowHeights', () => {
  it('knows where rows start and which row is at an offset', () => {
    const rows = new RowHeights([32, 32, 52, 32])
    expect(rows.count).toBe(4)
    expect(rows.total).toBe(148)
    expect([0, 1, 2, 3, 4].map((i) => rows.start(i))).toEqual([0, 32, 64, 116, 148])
    expect([0, 31, 32, 64, 115, 116, 147].map((y) => rows.at(y))).toEqual([0, 0, 1, 2, 2, 3, 3])
    // Outside the list: the first and the last.
    expect(rows.at(-10)).toBe(0)
    expect(rows.at(10_000)).toBe(3)
  })

  it('is empty without rows', () => {
    const rows = new RowHeights([])
    expect(rows.total).toBe(0)
    expect(rows.at(50)).toBe(0)
    expect(rows.measure(0, [32], 0)).toEqual({ changed: false, shift: 0 })
  })

  it('takes measurements and moves the rows after them', () => {
    const rows = new RowHeights([32, 32, 32, 32])
    expect(rows.measure(1, [72, 32], 0)).toEqual({ changed: true, shift: 0 })
    expect([0, 1, 2, 3, 4].map((i) => rows.start(i))).toEqual([0, 32, 104, 136, 168])
    expect(rows.at(100)).toBe(1)
    // Measured the same again: nothing to do, which is what ends the list's
    // measure-and-draw loop.
    expect(rows.measure(1, [72, 32], 0)).toEqual({ changed: false, shift: 0 })
    // Less than half a pixel is not a difference.
    expect(rows.measure(0, [32.3], 0).changed).toBe(false)
  })

  it('says by how much the rows above the view grew', () => {
    const rows = new RowHeights([32, 32, 32, 32, 32])
    // The view starts at 96, the top of the fourth row: the first three are
    // above it, the fourth is in it.
    const { changed, shift } = rows.measure(0, [52, 32, 22, 60], 96)
    expect(changed).toBe(true)
    // +20 for the first, -10 for the third; the fourth is in view and pushes
    // only what is below it.
    expect(shift).toBe(10)
    expect(rows.total).toBe(52 + 32 + 22 + 60 + 32)
  })

  it('ignores rows that are not laid out and rows outside the list', () => {
    const rows = new RowHeights([32, 32])
    expect(rows.measure(0, [0, 0], 0)).toEqual({ changed: false, shift: 0 })
    expect(rows.measure(1, [32, 99, 99], 0)).toEqual({ changed: false, shift: 0 })
    expect(rows.total).toBe(64)
  })
})
