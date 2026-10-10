import type {
  GuestAttachment,
  GuestMemory,
  GuestMessage,
  GuestRunRequest,
  Memory,
  ReasoningEffort,
} from '@openchat/api-client'

import { GuestError } from './errors'
import { DEFAULT_TITLE } from './repo'
import { transcriptOf } from './tree'
import type { ConversationData, StoredBlob, StoredMessage, StoredPersonalization } from './types'

/**
 * What a guest's run is asked with. The server keeps nothing between requests,
 * so each carries what an account's run would have read from the database: the
 * branch shown, the new question (or which answer to make again, or the answers
 * to the model's question), the person's instructions and memories, and the
 * memory snapshot the system prompt was rendered from.
 */

/** The most rows a request carries. A run reads the last 200; a few more are sent, never the whole of a long conversation. */
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
  | { action: 'edit'; content: string; messageId: string; id: string }
  | { action: 'regenerate' }
  | { action: 'answer'; callId: string; answers: { selected: string[]; other?: string }[] }

export interface BuiltRun {
  request: GuestRunRequest
  /** The question being asked, as the page will keep it once the run starts. */
  input?: { id: string; parentInputId: string | null; blobs: StoredBlob[] }
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
  const rows = transcriptOf(data)
  let history: StoredMessage[] = rows
  let input: BuiltRun['input']
  let inputRow: StoredMessage | undefined
  let editOf: string | undefined
  const extra: Partial<GuestRunRequest> = {}

  switch (what.action) {
    case 'send': {
      const blobs = await Promise.all(what.attachmentIds.map((id) => params.readBlob(id)))
      if (blobs.some((b) => !b)) {
        throw invalid('attachment_unavailable', 'a file was never uploaded, was already sent, or is not yours', 400)
      }
      const files = blobs as StoredBlob[]
      input = { id: what.id, parentInputId: data.conversation.currentInputId, blobs: files }
      inputRow = userRow(what.id, what.content, files)
      break
    }
    case 'edit': {
      const at = rows.findIndex((m) => m.id === what.messageId && m.role === 'user')
      if (at < 0) {
        throw data.messages.some((m) => m.id === what.messageId && m.role === 'user')
          ? invalid('not_on_path', 'that message is not on the branch being shown', 409)
          : invalid('message_not_found', 'no such message in this conversation', 404)
      }
      const edited = rows[at]!
      history = rows.slice(0, at)
      editOf = edited.editOf ?? edited.id
      // An edit changes what was asked, not what was attached: the question keeps its files.
      const files: StoredBlob[] = []
      for (const file of edited.attachments ?? []) {
        const blob = await params.readBlob(file.id)
        if (blob) files.push(blob)
      }
      input = { id: what.id, parentInputId: edited.parentInputId ?? null, blobs: files }
      inputRow = userRow(what.id, what.content, files)
      break
    }
    case 'regenerate': {
      const last = rows.map((m) => m.role).lastIndexOf('user')
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

  // The newest files are sent, as the newest are what the model gets.
  let budget = MAX_FILE_BYTES
  const sent = new Set<string>()
  const wanted = [...history, ...(inputRow ? [inputRow] : [])]
  for (const row of [...wanted].reverse()) {
    for (const file of [...(row.attachments ?? [])].reverse()) {
      if (file.size <= budget) {
        budget -= file.size
        sent.add(file.id)
      }
    }
  }
  const messages: GuestMessage[] = []
  for (const row of history.slice(-MAX_ROWS)) messages.push(await toGuestMessage(row, sent, params.readBlob))
  const request: GuestRunRequest = {
    action: what.action === 'edit' ? 'send' : what.action,
    conversationId: data.conversation.id,
    temporary: data.conversation.temporary,
    titled: data.conversation.title !== DEFAULT_TITLE,
    messages,
    ...(inputRow ? { input: await toGuestMessage(inputRow, sent, params.readBlob) } : {}),
    ...(editOf ? { editOf } : {}),
    ...extra,
    ...(params.model ? { model: params.model } : {}),
    ...(params.reasoningEffort ? { reasoningEffort: params.reasoningEffort } : {}),
    ...(params.webSearch !== undefined ? { webSearch: params.webSearch } : {}),
    aboutYou: params.personalization.aboutYou,
    responseStyle: params.personalization.responseStyle,
    memoryEnabled: params.personalization.memoryEnabled,
    memories: params.memories.map(
      (m): GuestMemory => ({ id: m.id, content: m.content, source: m.source }),
    ),
    ...(data.conversation.memorySnapshot ? { memorySnapshot: data.conversation.memorySnapshot } : {}),
  }
  return { request, input }
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

async function toGuestMessage(
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
    ...(row.stopReason ? { stopReason: row.stopReason } : {}),
    ...(row.model ? { model: row.model } : {}),
    ...(row.errorCode ? { errorCode: row.errorCode } : {}),
    ...(row.usage ? { usage: row.usage } : {}),
  }
  if (row.role === 'user' && row.attachments?.length) {
    const files: GuestAttachment[] = []
    for (const file of row.attachments) {
      const base = { id: file.id, filename: file.filename, contentType: file.contentType, size: file.size }
      const blob = sent.has(file.id) ? await readBlob(file.id) : undefined
      files.push(blob ? { ...base, data: await toBase64(blob.blob) } : base)
    }
    out.attachments = files
  }
  return out
}
