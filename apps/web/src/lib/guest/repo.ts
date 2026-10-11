import type {
  Attachment,
  CompactionState,
  ContextSummary,
  ContextUsage,
  Conversation,
  ConversationPage,
  CreateConversationRequest,
  Memory,
  MemoryEvent,
  Message,
  MessagePage,
  Personalization,
  ReasoningEffort,
  Run,
  UpdateConversationRequest,
  UpdatePersonalizationRequest,
} from '@openchat/api-client'

import { GuestError, notFound } from './errors'
import { isBranched, pruneBranches } from './legacy'
import {
  addMemory,
  applyModelChanges,
  MAX_CONTENT,
  MAX_INSTRUCTIONS,
  MAX_MEMORIES,
  recentEvents,
  rewriteMemory,
  sortMemories,
  undoEvent,
} from './memory'
import type { GuestStorage } from './storage'
import { survivingSummary } from './summary'
import { byRow, contextOf, deletedBy, encodeCursor, pageOf, rowKey, timeKey, transcriptOf } from './transcript'
import type { ConversationData, StoredBlob, StoredConversation, StoredMessage, StoredPersonalization, StoredRun } from './types'

/** What a new conversation is called until it is titled; the server's default too. */
export const DEFAULT_TITLE = 'New chat'

/** The key of the memory list's line of work (`serial`); no conversation has an id like it. */
const MEMORIES = 'memories'
/** How long a temporary chat lives, from its creation. */
export const TEMPORARY_LIFETIME_MS = 24 * 3600 * 1000
/** A run whose page has said nothing for this long has no page any more. */
export const RUN_STALE_MS = 20_000

/** What the administrator allows, as `GET /guest/config` says; the repository applies it to the personalization it reports. */
export interface PersonalizationLimits {
  memoryAllowed: boolean
  instructionLimit: number
  memoryLimit: number
  memoryLength: number
}

export interface RepoOptions {
  now?: () => Date
  newId?: () => string
  /** An address the page can load a stored file from. */
  urlFor?: (blob: Blob, id: string) => string
  /** Lets go of an address `urlFor` made. */
  revokeUrl?: (url: string) => void
  limits?: () => PersonalizationLimits
}

const fallbackLimits: PersonalizationLimits = {
  memoryAllowed: true,
  instructionLimit: MAX_INSTRUCTIONS,
  memoryLimit: MAX_MEMORIES,
  memoryLength: MAX_CONTENT,
}

const EFFORTS: ReasoningEffort[] = ['auto', 'none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max']

/** Everything a started run changes, as the first batch of its stream tells it. */
export interface StartedRun {
  run: Run
  /** The question the run answers, as the server wrote it. */
  input: Message
  action: 'send' | 'regenerate' | 'answer'
  /** For a question that rewords another: the one it replaces. That question and every row after it are deleted. */
  replaces?: string
  /** For a new question: the files it carries. */
  attachments?: StoredBlob[]
  /** What the composer asked this run for, which the conversation remembers. */
  choice: { reasoningEffort?: ReasoningEffort; webSearch?: boolean }
}

/** Everything a guest's conversations amount to. */
export class GuestRepo {
  readonly storage: GuestStorage
  private readonly now: () => Date
  private readonly newId: () => string
  private readonly urlFor: (blob: Blob, id: string) => string
  private readonly revokeUrl: (url: string) => void
  private readonly limits: () => PersonalizationLimits
  private chains = new Map<string, Promise<unknown>>()
  private swept = 0
  private migration?: Promise<void>
  private urls = new Map<string, string>()

  constructor(storage: GuestStorage, options: RepoOptions = {}) {
    this.storage = storage
    this.now = options.now ?? (() => new Date())
    this.newId = options.newId ?? (() => crypto.randomUUID())
    this.urlFor = options.urlFor ?? ((blob) => URL.createObjectURL(blob))
    this.revokeUrl = options.revokeUrl ?? ((url) => URL.revokeObjectURL(url))
    this.limits = options.limits ?? (() => fallbackLimits)
  }

  /** Runs `fn` after everything queued before it for the same conversation: the work of one conversation is one line. */
  serial<T>(conversationId: string, fn: () => Promise<T>): Promise<T> {
    const queued = (this.chains.get(conversationId) ?? Promise.resolve()).then(fn, fn)
    this.chains.set(conversationId, queued.catch(() => undefined))
    return queued
  }

  // ------------------------------------------------------------ reading data

  private async load(id: string): Promise<ConversationData> {
    await this.expire()
    const conversation = await this.storage.conversation(id)
    if (!conversation) throw notFound()
    const [messages, runs] = await Promise.all([this.storage.messages(id), this.storage.runs(id)])
    return { conversation, messages, runs: this.repaired(runs) }
  }

  /** A run still going whose page has been gone for a while was never finished: it reads as stopped. */
  private repaired(runs: StoredRun[]): StoredRun[] {
    const cutoff = this.now().getTime() - RUN_STALE_MS
    return runs.map((r) =>
      r.status === 'running' && (r.heartbeatAt ?? 0) < cutoff
        ? { ...r, status: 'cancelled', finishedAt: r.finishedAt ?? new Date(r.heartbeatAt ?? 0).toISOString() }
        : r,
    )
  }

  /**
   * Data kept by a version with branches is brought to a line before anything
   * reads it (legacy.ts), once per page load.
   */
  private migrated(): Promise<void> {
    this.migration ??= this.pruneBranched().catch((error) => {
      console.warn('guest data: pruning branches failed', error)
      this.migration = undefined
    })
    return this.migration
  }

  private async pruneBranched() {
    for (const c of await this.storage.conversations()) {
      if (!isBranched(c)) continue
      const [messages, runs] = await Promise.all([this.storage.messages(c.id), this.storage.runs(c.id)])
      const pruned = pruneBranches(c, messages, runs)
      await this.storage.putMessages(pruned.messages)
      await this.storage.putRuns(pruned.runs)
      await this.storage.deleteMessages(pruned.deletedMessages)
      await this.storage.deleteRuns(pruned.deletedRuns)
      for (const id of pruned.deletedFiles) await this.dropBlob(id)
      // Last: until then the conversation still reads as branched, and a page
      // that died half way through is pruned again, to the same result.
      await this.storage.putConversation(pruned.conversation)
    }
  }

  /** Temporary chats are gone a day after they were made; checked as the data is read, at most once a minute. */
  private async expire() {
    await this.migrated()
    const at = this.now().getTime()
    if (at - this.swept < 60_000) return
    this.swept = at
    for (const c of await this.storage.conversations()) {
      if (c.temporary && at - Date.parse(c.createdAt) > TEMPORARY_LIFETIME_MS) await this.dropConversation(c.id)
    }
    // Files picked and never sent.
    for (const blob of await this.storage.blobsOf('')) {
      if (at - (blob.at ?? 0) > TEMPORARY_LIFETIME_MS) await this.dropBlob(blob.id)
    }
  }

  // ------------------------------------------------- files and their addresses

  /** Lets go of the address made for a file (it holds the file's bytes in memory until revoked). */
  private forgetUrl(fileId: string) {
    const url = this.urls.get(fileId)
    if (url === undefined) return
    this.urls.delete(fileId)
    this.revokeUrl(url)
  }

  /** Deletes a stored file and the address made for it. */
  private async dropBlob(id: string) {
    await this.storage.deleteBlob(id)
    this.forgetUrl(id)
  }

  /** Deletes a conversation, and the addresses made for the files it held. */
  private async dropConversation(id: string) {
    const [messages, blobs] = await Promise.all([this.storage.messages(id), this.storage.blobsOf(id)])
    await this.storage.deleteConversation(id)
    for (const message of messages) for (const file of message.attachments ?? []) this.forgetUrl(file.id)
    for (const blob of blobs) this.forgetUrl(blob.id)
  }

  // ---------------------------------------------------------- the DTOs

  private toConversation(c: StoredConversation): Conversation {
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    const { memorySnapshot: _b, summary: _s, ...rest } = c
    return { ...rest, reasoningEffort: rest.reasoningEffort ?? 'auto' }
  }

  private async attachmentsOf(rows: StoredMessage[]): Promise<Map<string, Attachment[]>> {
    const out = new Map<string, Attachment[]>()
    for (const row of rows) {
      if (!row.attachments?.length) continue
      const files: Attachment[] = []
      for (const file of row.attachments) {
        let url = this.urls.get(file.id)
        if (!url) {
          const blob = await this.storage.blob(file.id)
          if (!blob) continue
          url = this.urlFor(blob.blob, file.id)
          this.urls.set(file.id, url)
        }
        files.push({ ...file, url })
      }
      if (files.length) out.set(row.id, files)
    }
    return out
  }

  /** Rows for a client: each with its files. */
  private async messagePage(rows: StoredMessage[]): Promise<Message[]> {
    const attached = await this.attachmentsOf(rows)
    return rows.map((row) => {
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { attachments: _a, providerState: _ps, ...message } = row
      const out: Message = { ...message }
      const files = attached.get(row.id)
      if (files) out.attachments = files
      return out
    })
  }

  // ---------------------------------------------------------- conversations

  async listConversations(params: { limit?: number; cursor?: string; search?: string }): Promise<ConversationPage> {
    await this.expire()
    const limit = Math.min(Math.max(params.limit ?? 50, 1), 100)
    const search = (params.search ?? '').trim().slice(0, 200)
    let rows = (await this.storage.conversations()).filter((c) => !c.temporary)

    const hits = new Map<string, StoredMessage>()
    if (search) {
      const needle = search.toLocaleLowerCase()
      const kept: StoredConversation[] = []
      for (const c of rows) {
        // Not the messages moderation refused: they were never sent (SearchConversations).
        const found = contextOf(await this.storage.messages(c.id))
          .filter((m) => (m.role === 'user' || m.role === 'assistant') && m.content.toLocaleLowerCase().includes(needle))
          .sort(byRow)
          .at(-1)
        if (found) hits.set(c.id, found)
        if (found || c.title.toLocaleLowerCase().includes(needle)) kept.push(c)
      }
      rows = kept
    }

    const key = (c: StoredConversation) => `${timeKey(c.updatedAt)}|${c.id}`
    rows.sort((a, b) => (key(a) < key(b) ? 1 : -1))
    if (params.cursor) rows = rows.filter((c) => key(c) < params.cursor!)
    const page = rows.slice(0, limit)
    const last = page.at(-1)
    return {
      items: page.map((c) => {
        const hit = hits.get(c.id)
        const out = this.toConversation(c)
        if (hit && (hit.role === 'user' || hit.role === 'assistant')) {
          out.match = { messageId: hit.id, role: hit.role, excerpt: excerpt(hit.content, search) }
        }
        return out
      }),
      nextCursor: rows.length > limit && last ? key(last) : null,
    }
  }

  async createConversation(body: CreateConversationRequest): Promise<Conversation> {
    const at = this.now().toISOString()
    const title = body.title?.trim() || DEFAULT_TITLE
    if (body.reasoningEffort && !EFFORTS.includes(body.reasoningEffort)) {
      throw new GuestError(400, 'invalid_reasoning_effort', 'reasoningEffort must be auto or a reasoning level', 'reasoningEffort')
    }
    const conversation: StoredConversation = {
      id: this.newId(),
      title,
      model: body.model ?? null,
      reasoningEffort: body.reasoningEffort ?? 'auto',
      webSearch: body.webSearch ?? true,
      temporary: body.temporary === true,
      createdAt: at,
      updatedAt: at,
      memorySnapshot: null,
    }
    await this.storage.putConversation(conversation)
    return this.toConversation(conversation)
  }

  async getConversation(id: string): Promise<Conversation> {
    return this.toConversation((await this.load(id)).conversation)
  }

  async updateConversation(id: string, body: UpdateConversationRequest): Promise<Conversation> {
    if (body.title === undefined && body.model === undefined && body.reasoningEffort === undefined && body.webSearch === undefined) {
      throw new GuestError(400, 'nothing_to_update', 'give a title, a model, a reasoning effort or a web search choice')
    }
    if (body.reasoningEffort !== undefined && !EFFORTS.includes(body.reasoningEffort)) {
      throw new GuestError(400, 'invalid_reasoning_effort', 'reasoningEffort must be auto or a reasoning level', 'reasoningEffort')
    }
    if (body.title !== undefined && body.title.trim() === '') throw new GuestError(400, 'invalid_title', 'title must not be empty')
    if (body.model !== undefined && body.model.trim() === '') {
      throw new GuestError(400, 'model_unavailable', 'model must not be empty', 'model')
    }
    return this.serial(id, async () => {
      const { conversation } = await this.load(id)
      const next: StoredConversation = { ...conversation }
      if (body.title !== undefined) {
        next.title = body.title.trim()
        next.updatedAt = this.now().toISOString()
      }
      if (body.model !== undefined) next.model = body.model.trim()
      if (body.reasoningEffort !== undefined) next.reasoningEffort = body.reasoningEffort
      if (body.webSearch !== undefined) next.webSearch = body.webSearch
      await this.storage.putConversation(next)
      return this.toConversation(next)
    })
  }

  async deleteConversation(id: string): Promise<void> {
    return this.serial(id, async () => {
      await this.load(id)
      await this.dropConversation(id)
    })
  }

  /** Every conversation, temporary ones included. */
  async deleteAll(): Promise<number> {
    const all = await this.storage.conversations()
    for (const c of all) await this.dropConversation(c.id)
    for (const blob of await this.storage.blobsOf('')) await this.dropBlob(blob.id)
    return all.length
  }

  /** Which listed conversations are being answered right now. */
  async running(): Promise<string[]> {
    await this.expire()
    const listed = new Set((await this.storage.conversations()).filter((c) => !c.temporary).map((c) => c.id))
    return [
      ...new Set(
        this.repaired(await this.storage.allRuns())
          .filter((r) => r.status === 'running' && listed.has(r.conversationId))
          .map((r) => r.conversationId),
      ),
    ]
  }

  /** How much of the model's window the latest answered request took (LatestContextTokens). */
  async context(id: string): Promise<ContextUsage> {
    const { messages } = await this.load(id)
    const answered = messages.filter((m) => m.role === 'assistant' && m.usage).sort(byRow).at(-1)
    if (!answered?.usage) return {}
    const u = answered.usage
    return {
      usedTokens: u.inputTokens + (u.cacheReadTokens ?? 0) + (u.cacheWriteTokens ?? 0) + u.outputTokens,
      model: answered.model ?? null,
    }
  }

  // --------------------------------------------------------------- transcript

  async data(id: string): Promise<ConversationData> {
    return this.load(id)
  }

  async listMessages(id: string, params: { before?: string; limit?: number }): Promise<MessagePage> {
    const data = await this.load(id)
    const page = pageOf(transcriptOf(data), params.before, Math.min(Math.max(params.limit ?? 50, 1), 100))
    return { items: await this.messagePage(page.items), olderCursor: page.olderCursor }
  }

  /** A question's rows as the contract gives them, for the response to a send. */
  async rowsOf(rows: StoredMessage[]): Promise<Message[]> {
    return this.messagePage(rows)
  }

  /** The active run, if there is one (the repaired view). */
  activeRun(data: ConversationData): StoredRun | undefined {
    return data.runs.find((r) => r.status === 'running')
  }

  // --------------------------------------------------------------- run writes

  /**
   * A run has started: its question is kept (a new one, or the one being
   * answered again), its run too, and what the run replaces is deleted - for a
   * reworded question, the question it replaces and every row after it; for
   * a regeneration, the old answer, every run of it included. The new rows are
   * written before the old are deleted, so a page that dies between the two
   * leaves a conversation that reads as before.
   */
  async runStarted(conversationId: string, started: StartedRun, tab: string): Promise<void> {
    return this.serial(conversationId, async () => {
      const data = await this.load(conversationId)
      const { run, input } = started
      const at = this.now().toISOString()
      let question: StoredMessage | undefined

      if (started.action === 'send') {
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
        const { attachments: _a, ...row } = input
        const files = (started.attachments ?? []).map(({ id, filename, contentType, size }) => ({ id, filename, contentType, size }))
        question = { ...row, ...(files.length ? { attachments: files } : {}) }
        // The files were the page's to pick; now they are the conversation's.
        for (const blob of started.attachments ?? []) await this.storage.putBlob({ ...blob, conversationId })
      }

      const redo = started.replaces ? { from: started.replaces } : started.action === 'regenerate' ? { after: input.id } : undefined
      const gone = redo ? deletedBy(data.messages, redo) : []
      const goneIds = new Set(gone.map((m) => m.id))

      // A summary tied to a row that is deleted goes with it, as on the server.
      const remaining = [...data.messages.filter((m) => !goneIds.has(m.id)), ...(question ? [question] : [])]
      const summary = survivingSummary(data.conversation.summary, remaining) ?? null

      const stored: StoredRun = { ...run, inputId: input.id, tab, heartbeatAt: this.now().getTime() }
      await this.storage.putRuns([stored])
      if (question) await this.storage.putMessages([question])
      const next: StoredConversation = {
        ...data.conversation,
        summary,
        model: run.model,
        updatedAt: at,
        ...(started.choice.reasoningEffort ? { reasoningEffort: started.choice.reasoningEffort } : {}),
        ...(started.choice.webSearch !== undefined ? { webSearch: started.choice.webSearch } : {}),
      }
      await this.storage.putConversation(next)

      if (gone.length > 0 || redo) {
        // A regeneration deletes every run of the question, those that carried on after a question included.
        const runs = data.runs.filter((r) => goneIds.has(r.inputId) || (started.action === 'regenerate' && r.inputId === input.id))
        await this.storage.deleteMessages([...goneIds])
        await this.storage.deleteRuns(runs.map((r) => r.id))
        // The files of the questions that went, except those the new question carries.
        const carried = new Set(question?.attachments?.map((f) => f.id))
        for (const message of gone) {
          for (const file of message.attachments ?? []) if (!carried.has(file.id)) await this.dropBlob(file.id)
        }
      }
    })
  }

  /**
   * Keeps a message the server's moderation refused (`content_blocked` to a
   * send): a user row of status `blocked` at the end of the conversation, so
   * that it is still there after the page is opened again, as the server keeps
   * an account's. It is shown and never sent (transcript.ts, summary.ts,
   * request.ts), starts no run and carries no file - the files stay the
   * composer's.
   *
   * Written nowhere a run is writing, like the server's, and returns null then.
   * The rows are the server's, stamped by its clock and not the page's, but this
   * one is not the server's: it goes after the newest row there is, as the failed
   * reply of an interrupted run does (`runInterrupted`), so that it sorts last.
   */
  async keepBlocked(conversationId: string, content: string): Promise<Message | null> {
    return this.serial(conversationId, async () => {
      const data = await this.load(conversationId)
      if (this.activeRun(data)) return null
      const newest = transcriptOf(data).at(-1)
      const at = new Date(Math.max(this.now().getTime(), newest ? Date.parse(newest.createdAt) + 1 : 0)).toISOString()
      const row: StoredMessage = {
        id: this.newId(),
        conversationId,
        role: 'user',
        content,
        parts: [{ type: 'text', text: content }],
        status: 'blocked',
        createdAt: at,
      }
      await this.storage.putMessages([row])
      return (await this.messagePage([row]))[0] ?? null
    })
  }

  async messageAppended(conversationId: string, message: Message): Promise<void> {
    return this.serial(conversationId, async () => {
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { attachments: _a, ...row } = message
      await this.storage.putMessages([row])
    })
  }

  /** A run's state changed (it finished): kept as the server reports it. */
  async runUpdated(conversationId: string, run: Run): Promise<void> {
    return this.serial(conversationId, async () => {
      const held = (await this.storage.runs(conversationId)).find((r) => r.id === run.id)
      if (!held) return
      await this.storage.putRuns([{ ...held, ...run, heartbeatAt: this.now().getTime() }])
    })
  }

  /** Said by the page following a run, now and then, so that a run whose page died can be told from one that is slow. */
  async heartbeat(conversationId: string, runId: string): Promise<void> {
    const held = (await this.storage.runs(conversationId)).find((r) => r.id === runId)
    if (held?.status === 'running') await this.storage.putRuns([{ ...held, heartbeatAt: this.now().getTime() }])
  }

  /** What the last frame of a run brings: the snapshot, the memories as they are now, the log, the title. */
  async runEnded(
    conversationId: string,
    end: {
      run: Run
      memorySnapshot: Record<string, unknown> | null
      memories: Memory[]
      memoryEvents: MemoryEvent[]
      title?: string
      /** The compressed context as the run left it (the same as `compaction_finished` told, when it made one). */
      summary?: ContextSummary
    },
  ): Promise<void> {
    await this.serial(conversationId, async () => {
      const conversation = await this.storage.conversation(conversationId)
      if (conversation) {
        const next: StoredConversation = { ...conversation, memorySnapshot: end.memorySnapshot ?? conversation.memorySnapshot }
        // Absent means the server sent none back, which is not the same as the
        // conversation having none: a summary is only ever replaced here.
        if (end.summary) next.summary = end.summary
        if (end.title && conversation.title === DEFAULT_TITLE) next.title = end.title
        await this.storage.putConversation(next)
      }
      const held = (await this.storage.runs(conversationId)).find((r) => r.id === end.run.id)
      if (held) await this.storage.putRuns([{ ...held, ...end.run }])
    })
    // The memory list is the guest's, not the conversation's: the tool may have
    // changed it. What the model changed is applied to the list as it is now -
    // not the server's whole list, which is the list as the run began and would
    // undo what the person did while the answer streamed.
    await this.serial(MEMORIES, async () => {
      const { put, remove, events } = applyModelChanges(await this.storage.memories(), end.memoryEvents, end.memories)
      for (const id of remove) await this.storage.deleteMemory(id)
      for (const memory of put) await this.storage.putMemory({ ...memory, conversationId: memory.conversationId ?? null })
      for (const event of events) await this.storage.putMemoryEvent(event)
    })
  }

  /**
   * The stream a run came on ended without its last frame: the connection was
   * lost. The run is over as far as the page can tell - failed, with an empty
   * failed reply so that the turn says so and can be answered again.
   */
  async runInterrupted(conversationId: string, runId: string, code: string): Promise<{ run: Run; message: Message } | null> {
    return this.serial(conversationId, async () => {
      const held = (await this.storage.runs(conversationId)).find((r) => r.id === runId)
      if (!held || held.status !== 'running') return null
      // After the newest row there is: the rows are the server's, stamped by its
      // clock, and the page's would otherwise sort the failed reply before the
      // question it is the reply to.
      const newest = (await this.storage.messages(conversationId)).sort(byRow).at(-1)
      const at = new Date(Math.max(this.now().getTime(), newest ? Date.parse(newest.createdAt) + 1 : 0)).toISOString()
      const run: StoredRun = { ...held, status: 'failed', errorCode: code, finishedAt: at, version: held.version + 1 }
      const message: Message = {
        id: this.newId(),
        conversationId,
        role: 'assistant',
        content: '',
        parts: [],
        status: 'failed',
        runId,
        stopReason: 'error',
        errorCode: code,
        model: held.model,
        createdAt: at,
      }
      await this.storage.putRuns([run])
      await this.storage.putMessages([message])
      return { run, message }
    })
  }

  // ------------------------------------------------------- compressed context

  /** The conversation's summary, if it still describes the rows it is tied to. */
  async summaryOf(data: ConversationData): Promise<ContextSummary | undefined> {
    return survivingSummary(data.conversation.summary, data.messages)
  }

  async compaction(id: string): Promise<CompactionState> {
    const data = await this.load(id)
    return { summary: await this.summaryOf(data) }
  }

  /**
   * Keeps the conversation's summary - or none. A summary of rows that were
   * deleted meanwhile (an edit made while it was being written) is not kept.
   * Returns what the conversation holds after.
   */
  async setSummary(id: string, summary: ContextSummary | undefined): Promise<ContextSummary | undefined> {
    return this.serial(id, async () => {
      const data = await this.load(id)
      const kept = survivingSummary(summary, data.messages)
      await this.storage.putConversation({ ...data.conversation, summary: kept ?? null })
      return kept
    })
  }

  // ------------------------------------------------------------------ files

  async putFile(blob: StoredBlob): Promise<void> {
    await this.storage.putBlob({ ...blob, at: this.now().getTime() })
  }

  async file(id: string): Promise<StoredBlob | undefined> {
    return this.storage.blob(id)
  }

  // ----------------------------------------------------- personalization

  async personalization(): Promise<Personalization> {
    const held = (await this.storage.personalization()) ?? { aboutYou: '', responseStyle: '', memoryEnabled: true }
    const limits = this.limits()
    return {
      aboutYou: held.aboutYou,
      responseStyle: held.responseStyle,
      memoryEnabled: held.memoryEnabled,
      memoryAllowed: limits.memoryAllowed,
      instructionLimit: limits.instructionLimit,
      memoryLimit: limits.memoryLimit,
      memoryLength: limits.memoryLength,
    }
  }

  async storedPersonalization(): Promise<StoredPersonalization> {
    const { aboutYou, responseStyle, memoryEnabled } = await this.personalization()
    return { aboutYou, responseStyle, memoryEnabled }
  }

  async updatePersonalization(body: UpdatePersonalizationRequest): Promise<Personalization> {
    const held = await this.storedPersonalization()
    const limit = this.limits().instructionLimit
    for (const [field, value] of [['aboutYou', body.aboutYou], ['responseStyle', body.responseStyle]] as const) {
      if (value !== undefined && [...value].length > limit) {
        throw new GuestError(400, 'instructions_too_long', 'the instructions are too long', field)
      }
    }
    await this.storage.putPersonalization({
      aboutYou: body.aboutYou ?? held.aboutYou,
      responseStyle: body.responseStyle ?? held.responseStyle,
      memoryEnabled: body.memoryEnabled ?? held.memoryEnabled,
    })
    return this.personalization()
  }

  // ---------------------------------------------------------------- memory

  async memories(): Promise<Memory[]> {
    return sortMemories(await this.storage.memories())
  }

  // The list is one line of work, apart from the conversations': a person's edit
  // and the end of a run (which merges what the model changed) must not
  // interleave their reads and writes.

  async createMemory(content: string): Promise<Memory> {
    return this.serial(MEMORIES, async () => {
      const { memory } = addMemory(await this.storage.memories(), content, this.newId(), this.now())
      await this.storage.putMemory(memory)
      return memory
    })
  }

  async updateMemory(id: string, content: string): Promise<Memory> {
    return this.serial(MEMORIES, async () => {
      const next = rewriteMemory(await this.storage.memories(), id, content, this.now())
      await this.storage.putMemory(next)
      // A person's own edit: what the log holds about the note goes.
      await this.storage.deleteMemoryEvents((e) => e.memoryId === id)
      return next
    })
  }

  async deleteMemory(id: string): Promise<void> {
    return this.serial(MEMORIES, async () => {
      if (!(await this.storage.memories()).some((m) => m.id === id)) throw notFound()
      await this.storage.deleteMemory(id)
      await this.storage.deleteMemoryEvents((e) => e.memoryId === id)
    })
  }

  async clearMemories(): Promise<void> {
    return this.serial(MEMORIES, async () => {
      await this.storage.clearMemories()
      await this.storage.deleteMemoryEvents(() => true)
    })
  }

  async memoryEvents(): Promise<MemoryEvent[]> {
    return recentEvents(await this.storage.memoryEvents(), this.now())
  }

  async undoMemoryEvent(eventId: string): Promise<MemoryEvent> {
    return this.serial(MEMORIES, async () => {
      const events = await this.storage.memoryEvents()
      const { list, event } = undoEvent(await this.storage.memories(), events, eventId, this.now())
      const before = new Map((await this.storage.memories()).map((m) => [m.id, m]))
      for (const memory of list) if (before.get(memory.id) !== memory) await this.storage.putMemory(memory)
      for (const id of before.keys()) if (!list.some((m) => m.id === id)) await this.storage.deleteMemory(id)
      await this.storage.putMemoryEvent(event)
      return event
    })
  }

  // ------------------------------------------------------------ the data page

  /** The kept conversations as one document (ConversationExport): the text, and the names of the files. */
  async export() {
    const now = this.now()
    const conversations = (await this.storage.conversations()).filter((c) => !c.temporary)
    conversations.sort((a, b) => (timeKey(a.updatedAt) < timeKey(b.updatedAt) ? 1 : -1))
    const out = []
    for (const c of conversations) {
      const data = await this.load(c.id).catch(() => undefined)
      if (!data) continue
      // Not the messages moderation refused: the export is what was sent (ListTranscript).
      const messages = contextOf(transcriptOf(data))
        .filter((m) => m.role !== 'tool' && !(m.role === 'assistant' && m.content.trim() === ''))
        .map((m) => ({
          id: m.id,
          role: m.role as 'user' | 'assistant',
          content: m.content,
          createdAt: m.createdAt,
          ...(m.role === 'assistant' && m.model ? { model: m.model } : {}),
          ...(m.role === 'user' && m.attachments?.length
            ? { attachments: m.attachments.map(({ contentType, filename, size }) => ({ contentType, filename, size })) }
            : {}),
        }))
      out.push({ id: c.id, title: c.title, model: c.model ?? null, createdAt: c.createdAt, updatedAt: c.updatedAt, messages })
    }
    return { version: 1, exportedAt: now.toISOString(), conversations: out }
  }
}

const EXCERPT_BEFORE = 24
const EXCERPT_AFTER = 96

/** Text on one line, cut to the words around the first occurrence of the search, ignoring case (excerpt in conversation_search.go). */
export function excerpt(text: string, needle: string): string {
  const runes = [...text.split(/\s+/).filter(Boolean).join(' ')]
  const wanted = [...needle.split(/\s+/).filter(Boolean).join(' ')]
  const lower = (r: string) => r.toLocaleLowerCase()
  let at = -1
  for (let i = 0; wanted.length > 0 && i + wanted.length <= runes.length && at < 0; i++) {
    if (wanted.every((r, j) => lower(runes[i + j] ?? '') === lower(r))) at = i
  }
  if (at < 0) at = 0
  const start = Math.max(0, at - EXCERPT_BEFORE)
  const end = Math.min(runes.length, at + [...needle].length + EXCERPT_AFTER)
  return `${start > 0 ? '…' : ''}${runes.slice(start, end).join('')}${end < runes.length ? '…' : ''}`
}

export { encodeCursor, rowKey }
