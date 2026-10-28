/**
 * A passage quoted into a message: of a writing block, so the model changes
 * that passage and nothing else ("提出修改请求" in DocumentPanel), or of an
 * earlier answer, for the person's question about it ("引用提问", the
 * selection bubble on answers). The quote travels inside the message's text,
 * as a tag the model reads and the page draws as a card (UserMessage): nothing
 * new on the server, and the transcript, export and edit of the message carry
 * it as they carry any text.
 */

export interface DocumentQuote {
  /** The writing block quoted; empty for a passage of an answer. */
  documentId: string
  title: string
  /** Markdown, exactly as it stands in the document, occurring there once. */
  passage: string
}

/**
 * The first of the candidates - the selection as Markdown, then the whole
 * blocks it touches - that occurs exactly once in the document: what the
 * model's edit must find (document_tool.go wants a passage that occurs once).
 */
export function passageFor(markdown: string, candidates: string[]): string | null {
  for (const candidate of candidates) {
    const text = candidate.trim()
    if (text && occurrences(markdown, text) === 1) return text
  }
  return null
}

function occurrences(haystack: string, needle: string): number {
  let count = 0
  for (let at = haystack.indexOf(needle); at !== -1; at = haystack.indexOf(needle, at + 1)) count++
  return count
}

const attr = (value: string) => value.replace(/["\n]/g, ' ')

/** A passage of an answer, quoted for a question about it. */
export function answerQuote(passage: string): DocumentQuote {
  return { documentId: '', title: '', passage: passage.trim() }
}

/** The message: the quoted passage, then what the person asks of it. */
export function quoteMessage(quote: DocumentQuote, request: string): string {
  if (!quote.documentId) return `<quote>\n${quote.passage}\n</quote>\n\n${request}`
  return `<document-selection id="${attr(quote.documentId)}" title="${attr(quote.title)}">\n${quote.passage}\n</document-selection>\n\n${request}`
}

const QUOTE = /^<document-selection id="([^"]*)" title="([^"]*)">\n([\s\S]*?)\n<\/document-selection>\n\n?/
const ANSWER_QUOTE = /^<quote>\n([\s\S]*?)\n<\/quote>\n\n?/

/** A message's quote and the rest of it, or null when it quotes nothing. */
export function parseQuote(content: string): { quote: DocumentQuote; request: string } | null {
  const match = QUOTE.exec(content)
  if (match) {
    return { quote: { documentId: match[1]!, title: match[2]!, passage: match[3]! }, request: content.slice(match[0].length) }
  }
  const answer = ANSWER_QUOTE.exec(content)
  if (answer) return { quote: answerQuote(answer[1]!), request: content.slice(answer[0].length) }
  return null
}
