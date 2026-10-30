import { findPassage } from '@openchat/chat-core/find-passage'
import { smoothScrollBehavior } from './motion'

/** The name the highlight is drawn under (::highlight(n-quote) in agent.css). */
const NAME = 'n-quote'

/**
 * Shows a quoted passage where it is in the transcript: the newest answer that
 * holds it gets it highlighted - with the CSS Custom Highlight API, so the
 * answer's DOM is not touched - and scrolled into view. Null takes the
 * highlight away. Returns whether the passage was found; where the API is
 * missing, it is only scrolled to.
 */
export function highlightPassage(passage: string | null): boolean {
  const highlights = typeof CSS !== 'undefined' && 'highlights' in CSS ? CSS.highlights : null
  highlights?.delete(NAME)
  if (!passage) return false
  const answers = [...document.querySelectorAll<HTMLElement>('.n-column .n-msg-assistant')].reverse()
  for (const answer of answers) {
    const range = rangeOf(answer, passage)
    if (!range) continue
    if (highlights && typeof Highlight !== 'undefined') highlights.set(NAME, new Highlight(range))
    const box = range.getBoundingClientRect()
    const scroller = answer.closest('.n-scroll')
    if (scroller) {
      const view = scroller.getBoundingClientRect()
      scroller.scrollBy({ top: box.top - view.top - view.height / 3, behavior: smoothScrollBehavior() })
    }
    return true
  }
  return false
}

/** The range of the passage within one answer's text, or null. */
function rangeOf(root: HTMLElement, passage: string): Range | null {
  const nodes: Text[] = []
  const starts: number[] = []
  let text = ''
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    nodes.push(node as Text)
    starts.push(text.length)
    text += node.nodeValue ?? ''
  }
  const found = findPassage(text, passage)
  if (!found) return null
  const locate = (offset: number, end: boolean) => {
    // The node holding the offset: for an end, the one it closes.
    let i = 0
    for (let j = 0; j < starts.length; j++) {
      if (end ? starts[j]! < offset : starts[j]! <= offset) i = j
    }
    return { node: nodes[i]!, offset: offset - starts[i]! }
  }
  const start = locate(found[0], false)
  const end = locate(found[1], true)
  const range = document.createRange()
  range.setStart(start.node, start.offset)
  range.setEnd(end.node, end.offset)
  return range
}
