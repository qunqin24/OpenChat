/**
 * Splits an answer's Markdown into prose and `openchat-ui` blocks, in order.
 *
 * Done here rather than inside the Markdown renderer so a block can be drawn
 * while it is still streaming: an unclosed fence at the end of a streaming
 * answer is a block in progress, not a code sample.
 */
export type Segment =
  | { kind: 'markdown'; text: string }
  | { kind: 'ui'; source: string; complete: boolean }

const OPEN = /^```openchat-ui[ \t]*$/m

export function splitSegments(text: string, streaming: boolean): Segment[] {
  const segments: Segment[] = []
  let rest = text
  while (rest.length > 0) {
    const open = OPEN.exec(rest)
    if (!open) {
      segments.push({ kind: 'markdown', text: rest })
      break
    }
    if (open.index > 0) segments.push({ kind: 'markdown', text: rest.slice(0, open.index) })
    const body = rest.slice(open.index + open[0].length + 1)
    const close = /^```[ \t]*$/m.exec(body)
    if (!close) {
      // Unclosed: still being written, or the model never closed it. Either
      // way the declarations so far are what there is.
      segments.push({ kind: 'ui', source: body, complete: !streaming })
      break
    }
    segments.push({ kind: 'ui', source: body.slice(0, close.index), complete: true })
    rest = body.slice(close.index + close[0].length)
  }
  return segments.filter((s) => s.kind === 'ui' || s.text.trim() !== '')
}
