import { parseQuotes } from './document-selection'
import { turnText, type Turn } from './transcript'

/**
 * The conversation at a glance, for the navigator beside the transcript: one
 * entry per thing the user said, with the start of what was said back. Only
 * the turns that are loaded (the transcript pages from its end).
 */
export interface OutlineEntry {
  /** The user turn's key, which the page marks its message with. */
  key: string
  /** What the user asked, on one line; their files' names when they wrote nothing. */
  question: string
  /** The start of the answer as plain text; empty while there is none. */
  answer: string
}

/** How much of an answer an entry keeps: more than a preview shows. */
const ANSWER_CHARS = 280

export function turnOutline(turns: readonly Turn[]): OutlineEntry[] {
  const entries: OutlineEntry[] = []
  turns.forEach((turn, i) => {
    if (turn.kind !== 'user') return
    const { message } = turn
    // A request about a quoted passage is its words, not the passage.
    const words = parseQuotes(message.content)?.request ?? message.content
    const files = (message.attachments ?? []).map((file) => file.filename)
    const reply = turns[i + 1]
    entries.push({
      key: turn.key,
      question: oneLine(words) || files.join(', '),
      answer: reply?.kind === 'assistant' ? plainText(turnText(reply)).slice(0, ANSWER_CHARS) : '',
    })
  })
  return entries
}

function oneLine(text: string): string {
  return text.replace(/\s+/g, ' ').trim()
}

/**
 * Markdown read as the words it shows, roughly: enough for a two-line preview,
 * not a renderer. Code blocks, images and math markup go; links keep their
 * text; marks for emphasis, headings, lists, quotes and tables go.
 */
export function plainText(markdown: string): string {
  return oneLine(
    markdown
      .replace(/```[\s\S]*?(```|$)/g, ' ')
      .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
      .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
      .replace(/<[^>]+>/g, ' ')
      .replace(/^\s{0,3}(#{1,6}\s+|>\s?|[-*+]\s+|\d+[.)]\s+)/gm, '')
      .replace(/^\s*\|?\s*:?-{3,}.*$/gm, ' ')
      .replace(/\|/g, ' ')
      .replace(/(\*\*|__|~~|`|\$\$?)/g, '')
      .replace(/(^|\W)[*_](\S[^*_]*?)[*_](?=\W|$)/g, '$1$2'),
  )
}
