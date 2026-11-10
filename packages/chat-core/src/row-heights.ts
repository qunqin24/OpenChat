/**
 * The heights of a long list's rows when only the rows in view are drawn and a
 * row is as tall as its content makes it: an estimate for each to begin with,
 * replaced by what the row measures once it has been drawn. From them, where
 * each row starts and which row is at an offset - what the list needs to know
 * which rows to draw and how much room to leave for the rest.
 *
 * Mutable, and not React state: the list measures after every draw and asks
 * again only when a measurement differed.
 */
export class RowHeights {
  private readonly heights: Float64Array
  /** starts[i] is where row i begins; one more than the rows, the last the total. */
  private readonly starts: Float64Array
  /** The starts are right up to this row; the ones after it are worked out on demand. */
  private settled = 0

  constructor(estimates: ArrayLike<number>) {
    this.heights = Float64Array.from(estimates)
    this.starts = new Float64Array(this.heights.length + 1)
  }

  get count(): number {
    return this.heights.length
  }

  /** The height of all the rows. */
  get total(): number {
    return this.start(this.count)
  }

  /** Where a row begins; for the count, where the last one ends. */
  start(row: number): number {
    this.settle()
    return this.starts[Math.min(this.count, Math.max(0, row))]!
  }

  /** The row that holds an offset; the first or the last for one outside the list. */
  at(offset: number): number {
    this.settle()
    let low = 0
    let high = this.count - 1
    while (low < high) {
      const middle = (low + high + 1) >> 1
      if (this.starts[middle]! <= offset) low = middle
      else high = middle - 1
    }
    return Math.max(0, low)
  }

  /**
   * Takes the heights the rows from `first` on were measured at. `changed` says
   * whether any differed from what was held; `shift` is by how much the rows
   * that end at or above `viewTop` grew in all, which is what the view has to
   * be scrolled by for what is on screen to stay where it is. A height of zero
   * is a row that is not laid out (a hidden panel) and is ignored.
   */
  measure(first: number, measured: ArrayLike<number>, viewTop: number): { changed: boolean; shift: number } {
    this.settle()
    let shift = 0
    let from = -1
    for (let k = 0; k < measured.length; k++) {
      const row = first + k
      if (row < 0 || row >= this.count) continue
      const height = measured[k]!
      const held = this.heights[row]!
      if (!(height > 0) || Math.abs(height - held) < 0.5) continue
      // Where the row was, by the heights held until now.
      if (this.starts[row]! + held <= viewTop) shift += height - held
      this.heights[row] = height
      if (from < 0) from = row
    }
    if (from < 0) return { changed: false, shift: 0 }
    this.settled = Math.min(this.settled, from)
    return { changed: true, shift }
  }

  private settle(): void {
    for (let i = this.settled; i < this.count; i++) this.starts[i + 1] = this.starts[i]! + this.heights[i]!
    this.settled = this.count
  }
}
