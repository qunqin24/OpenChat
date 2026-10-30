import type { ToolCallPart, ToolResultPart } from '@openchat/api-client'

/** The tool models write writing blocks with (api/document_tool.go). */
export const DOCUMENT_TOOL = 'document'

/** What a succeeded create or update did: which document, at which version. */
export interface DocumentChange {
  op: 'create' | 'update'
  documentId: string
  version: number
  title: string
}

export function documentChangeOf(result: ToolResultPart | undefined): DocumentChange | null {
  if (result?.status !== 'succeeded') return null
  const d = result.details
  if (!d || typeof d !== 'object') return null
  if ((d.op !== 'create' && d.op !== 'update') || typeof d.documentId !== 'string') return null
  return {
    op: d.op,
    documentId: d.documentId,
    version: typeof d.version === 'number' ? d.version : 0,
    title: typeof d.title === 'string' ? d.title : '',
  }
}

/** The title a create call is writing, as far as its arguments have streamed. */
export function pendingTitle(call: ToolCallPart): string {
  const args = call.arguments as { title?: unknown } | undefined
  if (typeof args?.title === 'string') return args.title
  const match = /"title"\s*:\s*"((?:[^"\\]|\\.)*)/.exec(call.rawArguments ?? '')
  if (!match) return ''
  try {
    return JSON.parse(`"${match[1]}"`) as string
  } catch {
    return match[1] ?? ''
  }
}

/** An edit the model is writing, as far as its arguments have streamed. */
export interface PendingEdit {
  documentId: string
  /** The passages being replaced, those whose text has arrived whole. */
  finds: string[]
  /** The whole text is being rewritten. */
  rewrite: boolean
}

const unquote = (raw: string) => {
  try {
    return JSON.parse(`"${raw}"`) as string
  } catch {
    return raw
  }
}

/** A JSON string's body cut off mid-stream, read as far as it goes. */
const unquotePartial = (raw: string) => {
  // A cut may fall inside an escape: drop the unfinished one.
  const trailing = /\\+$/.exec(raw)?.[0].length ?? 0
  const cut = (trailing % 2 === 1 ? raw.slice(0, -1) : raw).replace(/(^|[^\\])((?:\\\\)*)\\u[0-9a-fA-F]{0,3}$/, '$1$2')
  return unquote(cut)
}

/**
 * The text a create or update call is writing, as far as its arguments have
 * streamed: the whole `content`, or else the `replace` of each edit, so the
 * card in the reply can show the words as they come.
 */
export function streamingText(call: ToolCallPart): string {
  const args = call.arguments as { content?: unknown; edits?: { replace?: unknown }[] } | undefined
  if (typeof args?.content === 'string') return args.content
  if (Array.isArray(args?.edits)) return args.edits.map((e) => (typeof e.replace === 'string' ? e.replace : '')).filter(Boolean).join('\n\n')
  const raw = call.rawArguments ?? ''
  const content = /"content"\s*:\s*"((?:[^"\\]|\\.)*\\?)/.exec(raw)
  if (content) return unquotePartial(content[1] ?? '')
  return [...raw.matchAll(/"replace"\s*:\s*"((?:[^"\\]|\\.)*\\?)/g)].map((m) => unquotePartial(m[1] ?? '')).filter(Boolean).join('\n\n')
}

/**
 * Which passages an update call is about to replace, read from its arguments
 * while they stream: each `find` once its closing quote has come, so the panel
 * can mark them before the call finishes. Null for a call that is not an
 * update of a known document (yet).
 */
export function pendingEditOf(call: ToolCallPart): PendingEdit | null {
  const raw = call.rawArguments ?? ''
  const args = call.arguments as { op?: unknown; id?: unknown; content?: unknown; edits?: { find?: unknown }[] } | undefined
  const op = typeof args?.op === 'string' ? args.op : /"op"\s*:\s*"([a-z]+)"/.exec(raw)?.[1]
  if (op !== 'update') return null
  const id = typeof args?.id === 'string' ? args.id : /"id"\s*:\s*"([0-9a-f-]{36})"/.exec(raw)?.[1]
  if (!id) return null
  const finds = Array.isArray(args?.edits)
    ? args.edits.map((e) => (typeof e.find === 'string' ? e.find : '')).filter(Boolean)
    : [...raw.matchAll(/"find"\s*:\s*"((?:[^"\\]|\\.)*)"/g)].map((m) => unquote(m[1] ?? '')).filter(Boolean)
  return { documentId: id, finds, rewrite: typeof args?.content === 'string' || /"content"\s*:/.test(raw) }
}

/** Markdown reduced to the text it shows, to be found in the editor's blocks. */
export function shownText(markdown: string): string {
  return markdown
    .split('\n')
    .map((line) => line.replace(/^\s*(#{1,6}\s+|[-*+]\s+|\d+[.)]\s+|>\s?)/, ''))
    .join('\n')
    .replace(/(\*\*|__|\*|_|`|~~)/g, '')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
}

/**
 * The indexes of the blocks (paragraphs, headings, lists) a set of passages
 * touches: a block whose text holds a line of a passage, or is held by one.
 */
export function blocksTouched(blocks: string[], finds: string[]): number[] {
  const lines = finds.flatMap((find) => shownText(find).split('\n').map((l) => l.trim()).filter((l) => l.length >= 2))
  const hit = new Set<number>()
  blocks.forEach((text, i) => {
    const t = text.trim()
    if (t && lines.some((line) => t.includes(line) || line.includes(t))) hit.add(i)
  })
  return [...hit]
}

/**
 * What an update changed, for its card in the reply: the runs of paragraphs
 * that are new in `after`, each a passage, with `true` where unchanged text
 * was left out between them. The whole text (one passage) where most of it
 * changed, or nothing was added that can be shown on its own.
 */
export function changedPassages(before: string | undefined, after: string): (string | true)[] {
  const blocks = after.split(/\n[ \t]*\n/).filter((b) => b.trim())
  if (before === undefined) return [after]
  const old = new Set(before.split(/\n[ \t]*\n/).map((b) => b.trim()))
  const changed = blocks.map((b) => !old.has(b.trim()))
  const count = changed.filter(Boolean).length
  if (count === 0 || count / blocks.length > 0.6) return [after]
  const out: (string | true)[] = []
  let run: string[] = []
  blocks.forEach((block, i) => {
    if (changed[i]) {
      run.push(block)
      return
    }
    if (run.length) out.push(run.join('\n\n'))
    run = []
    if (out.at(-1) !== true) out.push(true)
  })
  if (run.length) out.push(run.join('\n\n'))
  return out
}
