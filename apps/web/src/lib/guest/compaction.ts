import type { ApiError, ContextSummary, GuestCompactRequest, GuestCompactResponse } from '@openchat/api-client'

import { GuestError } from './errors'
import type { GuestHub } from './hub'
import type { GuestRepo } from './repo'
import { toGuestMessage } from './request'
import { planCompaction } from './summary'
import { transcriptOf } from './transcript'

/**
 * Compressing a guest's conversation on request, and taking it back. The
 * conversation is in the browser, so the browser decides what is covered
 * (summary.ts, the same plan the server makes for an account) and the server
 * only has the model write the summary (`POST /guest/compaction`): it keeps
 * nothing. Compression during a run is part of the run, and needs none of this.
 */
export interface CompactionDeps {
  repo: GuestRepo
  hub: GuestHub
  /** The API's base URL: /guest/compaction is posted under it. */
  baseUrl: string
  /** The network's fetch, never the page's own (which would serve this locally). */
  fetch: typeof globalThis.fetch
  /** The conversation's summary changed: other pages showing it are told. */
  changed?: (conversationId: string) => void
  now?: () => Date
}

const busy = () => new GuestError(409, 'conversation_busy', 'this conversation is already answering')

export async function compactGuestConversation(deps: CompactionDeps, conversationId: string): Promise<ContextSummary> {
  const { repo } = deps
  const data = await repo.data(conversationId)
  if (repo.activeRun(data)) throw busy()

  const held = await repo.summaryOf(data)
  const plan = planCompaction(transcriptOf(data), held)
  if (!plan) throw new GuestError(409, 'nothing_to_compact', 'there is nothing new to compress')

  const request: GuestCompactRequest = {
    ...(data.conversation.model ? { model: data.conversation.model } : {}),
    ...(held ? { existing: held.content } : {}),
    // Files are named in the history and not sent: the summary does not need their bytes.
    messages: await Promise.all(plan.covered.map((row) => toGuestMessage(row, new Set(), async () => undefined))),
    ...(plan.keep ? { latest: plan.keep.content } : {}),
  }
  let response: Response
  try {
    response = await deps.fetch(`${deps.baseUrl.replace(/\/$/, '')}/guest/compaction`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
    })
  } catch {
    throw new GuestError(502, 'compaction_failed', 'the server could not be reached')
  }
  if (!response.ok) {
    const error = (await response.json().catch(() => undefined)) as ApiError | undefined
    throw new GuestError(response.status, error?.code ?? 'compaction_failed', error?.message ?? 'the model did not write a summary', error?.field)
  }
  const written = (await response.json()) as GuestCompactResponse

  const summary: ContextSummary = {
    content: written.content,
    throughMessageId: plan.through.id,
    ...(plan.keep ? { keptMessageId: plan.keep.id } : {}),
    model: written.model,
    inputTokens: written.inputTokens,
    outputTokens: written.outputTokens,
    createdAt: (deps.now?.() ?? new Date()).toISOString(),
  }
  // A run that started while the model was writing owns the conversation now;
  // and rows an edit deleted meanwhile are not described by a summary.
  if (repo.activeRun(await repo.data(conversationId))) throw busy()
  const kept = await repo.setSummary(conversationId, summary)
  if (!kept) throw busy()
  deps.hub.announceCompaction(conversationId, kept)
  deps.changed?.(conversationId)
  return kept
}

/** Takes the compression back: the model is sent the whole conversation again. */
export async function undoGuestCompaction(deps: CompactionDeps, conversationId: string): Promise<void> {
  const { repo } = deps
  const data = await repo.data(conversationId)
  if (repo.activeRun(data)) throw busy()
  if (!(await repo.summaryOf(data))) return
  await repo.setSummary(conversationId, undefined)
  deps.hub.announceCompaction(conversationId, undefined)
  deps.changed?.(conversationId)
}
