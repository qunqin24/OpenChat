import type {
  GuestAttachment,
  GuestMemory,
  GuestMessage,
  GuestRunRequest,
  Memory,
  ReasoningEffort,
} from '@openchat/api-client'

import { GuestError } from './errors'
import { requestReceipts, signedSummary } from './receipts'
import { DEFAULT_TITLE } from './repo'
import { rowsToSend, survivingSummary } from './summary'
import { contextOf, isBlocked, lastQuestionIndex, transcriptOf } from './transcript'
import type { ConversationData, StoredBlob, StoredMessage, StoredPersonalization } from './types'

/**
 * What a guest's run is asked with. The server keeps nothing between requests,
 * so each carries what an account's run would have read from the database: the
 * conversation, the new question (or which answer to make again, or the answers
 * to the model's question), the person's instructions and memories, and the
 * memory snapshot the system prompt was rendered from.
 */

/**
 * The most rows a request carries: the newest of the conversation, and never
 * the whole of a long one. A conversation that has a summary sends the rows
 * after it (and the two rows the summary needs, summary.ts); what keeps the
 * model's context within its window is compression, not this.
 */
export const MAX_ROWS = 300
/** The files one request can carry, as the model is sent them (agent.MaxAttachmentBytes); older files are named but not sent. */
export const MAX_FILE_BYTES = 20 << 20

export interface RunParams {
  data: ConversationData
  model?: string
  reasoningEffort?: ReasoningEffort
  webSearch?: boolean
  personalization: StoredPersonalization
  memories: Memory[]
  readBlob: (id: string) => Promise<StoredBlob | undefined>
}

export type RunAction =
  | { action: 'send'; content: string; attachmentIds: string[]; id: string }
  | { action: 'edit'; content: string; messageId: string; id: string; attachmentIds?: string[] }
  | { action: 'regenerate' }
  | { action: 'answer'; callId: string; answers: { selected: string[]; other?: string }[] }

export interface BuiltRun {
  request: GuestRunRequest
  /**
   * The question being asked, as the page will keep it once the run starts;
   * `replaces` is the question a reworded one takes the place of.
   */
  input?: {
    id: string
    replaces?: string
    blobs: StoredBlob[]
    /** The question replaced was a refused message: words refused again take its place. */
    replacesRefused?: boolean
  }
}

const invalid = (code: string, message: string, status = 400) => new GuestError(status, code, message)

/** A file as base64, without holding two copies of it as characters at once. */
export async function toBase64(blob: Blob): Promise<string> {
  const bytes = new Uint8Array(await blob.arrayBuffer())
  let binary = ''
  const step = 0x8000
  for (let i = 0; i < bytes.length; i += step) binary += String.fromCharCode(...bytes.subarray(i, i + step))
  return btoa(binary)
}

export async function buildRunRequest(params: RunParams, what: RunAction): Promise<BuiltRun> {
  const { data } = params
  // Every row, and the rows a model can be given: the messages moderation
  // refused are in the first and never in the second.
  const all = transcriptOf(data)
  const rows = contextOf(all)
  let history: StoredMessage[] = rows
  let input: BuiltRun['input']
  let inputRow: StoredMessage | undefined
  const extra: Partial<GuestRunRequest> = {}

  switch (what.action) {
    case 'send': {
      const blobs = await Promise.all(what.attachmentIds.map((id) => params.readBlob(id)))
      if (blobs.some((b) => !b)) {
        throw invalid('attachment_unavailable', 'a file was never uploaded, was already sent, or is not yours', 400)
      }
      const files = blobs as StoredBlob[]
      input = { id: what.id, blobs: files }
      inputRow = userRow(what.id, what.content, files)
      break
    }
    case 'edit': {
      // The question and everything after it are not part of what the new wording is asked from.
      // A refused message can be edited like any other: it is one of the rows.
      const at = all.findIndex((m) => m.id === what.messageId && m.role === 'user')
      if (at < 0) throw invalid('message_not_found', 'no such message in this conversation', 404)
      const edited = all[at]!
      history = contextOf(all.slice(0, at))
      const refused = isBlocked(edited)
      // A refused message carried no files (they stayed the composer's), so a
      // reworded one takes the composer's; any other keeps its own and takes none,
      // as on the server.
      if ((what.attachmentIds?.length ?? 0) > 0 && !refused) {
        throw invalid('attachments_not_editable', 'only a message that moderation refused takes files when it is edited', 400)
      }
      const files: StoredBlob[] = []
      if (refused) {
        const picked = await Promise.all((what.attachmentIds ?? []).map((id) => params.readBlob(id)))
        if (picked.some((b) => !b)) {
          throw invalid('attachment_unavailable', 'a file was never uploaded, was already sent, or is not yours', 400)
        }
        files.push(...(picked as StoredBlob[]))
      } else {
        // An edit changes what was asked, not what was attached: the question keeps its files.
        for (const file of edited.attachments ?? []) {
          const blob = await params.readBlob(file.id)
          if (blob) files.push(blob)
        }
      }
      input = { id: what.id, replaces: edited.id, blobs: files, ...(refused ? { replacesRefused: true } : {}) }
      inputRow = userRow(what.id, what.content, files)
      break
    }
    case 'regenerate': {
      // The last question asked, not a message refused after it.
      const last = lastQuestionIndex(rows)
      if (last < 0) throw invalid('nothing_to_answer', 'this conversation has no message to answer', 404)
      // The answer being made again is not part of what it is made from.
      history = rows.slice(0, last + 1)
      break
    }
    case 'answer': {
      const last = rows.at(-1)
      const asked = last?.role === 'assistant' && last.parts.some((p) => p.type === 'tool_call' && p.id === what.callId)
      if (!last || !asked) throw invalid('not_pending', 'this question is no longer waiting for an answer', 409)
      // The answer joins the run that asked, or the run that began the reply it is part of.
      const run = data.runs.find((r) => r.id === last.runId)
      extra.callId = what.callId
      extra.answers = what.answers
      extra.continues = run?.continues ?? run?.id ?? last.runId
      break
    }
  }

  // What the conversation's summary still describes once this run has deleted
  // what it replaces (an edit's question and what followed it, an old answer).
  // One the server did not sign is left out (receipts.ts).
  const summary = signedSummary(survivingSummary(data.conversation.summary, history))
  const carried = rowsToSend(history, summary, MAX_ROWS)

  // The newest files are sent, as the newest are what the model gets.
  let budget = MAX_FILE_BYTES
  const sent = new Set<string>()
  const wanted = [...carried, ...(inputRow ? [inputRow] : [])]
  for (const row of [...wanted].reverse()) {
    for (const file of [...(row.attachments ?? [])].reverse()) {
      if (file.size <= budget) {
        budget -= file.size
        sent.add(file.id)
      }
    }
  }
  const messages: GuestMessage[] = []
  for (const row of carried) messages.push(await toGuestMessage(row, sent, params.readBlob))
  const request: GuestRunRequest = {
    action: what.action === 'edit' ? 'send' : what.action,
    conversationId: data.conversation.id,
    temporary: data.conversation.temporary,
    titled: data.conversation.title !== DEFAULT_TITLE,
    messages,
    ...(inputRow ? { input: await toGuestMessage(inputRow, sent, params.readBlob) } : {}),
    ...extra,
    ...(params.model ? { model: params.model } : {}),
    ...(params.reasoningEffort ? { reasoningEffort: params.reasoningEffort } : {}),
    ...(params.webSearch !== undefined ? { webSearch: params.webSearch } : {}),
    aboutYou: params.personalization.aboutYou,
    responseStyle: params.personalization.responseStyle,
    memoryEnabled: params.personalization.memoryEnabled,
    memories: params.memories.map(
      (m): GuestMemory => ({ id: m.id, content: m.content, source: m.source, ...(m.receipt ? { receipt: m.receipt } : {}) }),
    ),
    ...receiptsOf(params, data),
    ...(data.conversation.memorySnapshot ? { memorySnapshot: data.conversation.memorySnapshot } : {}),
    ...(summary ? { summary } : {}),
  }
  return { request, input }
}

/** The server's signatures for the instructions and the snapshot, as the request carries them. */
function receiptsOf(params: RunParams, data: ConversationData): { receipts?: GuestRunRequest['receipts'] } {
  const receipts = requestReceipts(params.personalization, data.conversation.memorySnapshot ? data.conversation.memorySnapshotReceipt : undefined)
  return receipts ? { receipts } : {}
}

function userRow(id: string, content: string, files: StoredBlob[]): StoredMessage {
  return {
    id,
    conversationId: '',
    role: 'user',
    content,
    parts: [{ type: 'text', text: content }],
    status: 'complete',
    createdAt: new Date().toISOString(),
    ...(files.length
      ? { attachments: files.map(({ id: fileId, filename, contentType, size }) => ({ id: fileId, filename, contentType, size })) }
      : {}),
  }
}

export async function toGuestMessage(
  row: StoredMessage,
  sent: Set<string>,
  readBlob: RunParams['readBlob'],
): Promise<GuestMessage> {
  const out: GuestMessage = {
    id: row.id,
    role: row.role,
    content: row.content,
    parts: row.parts,
    createdAt: row.createdAt,
    ...(row.providerState ? { providerState: row.providerState } : {}),
    ...(row.receipt ? { receipt: row.receipt } : {}),
    ...(row.stopReason ? { stopReason: row.stopReason } : {}),
    ...(row.model ? { model: row.model } : {}),
    ...(row.errorCode ? { errorCode: row.errorCode } : {}),
    ...(row.usage ? { usage: row.usage } : {}),
  }
  if (row.role === 'user' && row.attachments?.length) {
    const files: GuestAttachment[] = []
    for (const file of row.attachments) {
      const base = {
        id: file.id,
        filename: file.filename,
        contentType: file.contentType,
        size: file.size,
        ...(file.receipt ? { receipt: file.receipt } : {}),
      }
      const blob = sent.has(file.id) ? await readBlob(file.id) : undefined
      files.push(blob ? { ...base, data: await toBase64(blob.blob) } : base)
    }
    out.attachments = files
  }
  return out
}
