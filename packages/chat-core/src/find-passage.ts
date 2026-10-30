/**
 * Where a quoted passage is in the text of an answer, for showing it there
 * (AskSelection's quotes, lib/highlight-passage.ts). The passage is what the
 * selection said - a line break between paragraphs, spaces as the page laid
 * them out - and the text is the answer's text nodes run together, so
 * whitespace is set aside on both sides and only the rest must match.
 *
 * Returns [start, end) in `text`, or null when the passage is not there.
 */
export function findPassage(text: string, passage: string): [number, number] | null {
  const wanted = passage.replace(/\s+/g, '')
  if (!wanted) return null
  // The text without its whitespace, and where each of its characters was.
  const at: number[] = []
  let squeezed = ''
  for (let i = 0; i < text.length; i++) {
    if (/\s/.test(text[i]!)) continue
    at.push(i)
    squeezed += text[i]
  }
  const found = squeezed.indexOf(wanted)
  if (found < 0) return null
  return [at[found]!, at[found + wanted.length - 1]! + 1]
}
