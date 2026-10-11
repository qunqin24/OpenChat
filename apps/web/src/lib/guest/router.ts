import type { GuestConfig, Message, ReasoningEffort } from '@openchat/api-client'

import { compactGuestConversation, undoGuestCompaction } from './compaction'
import { GuestError, notFound } from './errors'
import { eventStream, type GuestHub } from './hub'
import type { GuestRepo } from './repo'
import type { GuestRunner } from './runner'
import { transcriptOf } from './transcript'

/**
 * The API a guest's page calls, answered in the page. Everything the signed-in
 * chat asks of the server about conversations, messages, memories and the
 * person's own data is answered from the browser's storage (repo.ts) and
 * the run's events from the hub; what needs a model goes to the server through
 * the runner. The routes are the contract's, with the contract's bodies and
 * errors, so the hooks and components do not know which side they are talking
 * to.
 *
 * `handle` answers null for a request it does not serve, which the caller then
 * sends to the network.
 */
export interface GuestApiDeps {
  repo: GuestRepo
  runner: GuestRunner
  hub: GuestHub
  /** The API's base URL, for the few calls that go to the server. */
  baseUrl: string
  fetch: typeof globalThis.fetch
  /** What the server offers a guest; read once in a while. */
  config: () => Promise<GuestConfig>
  /** A conversation's summary was made or taken back: the guest's other pages are told. */
  compactionChanged?: (conversationId: string) => void
  /** Conversations were deleted: the guest's other pages are told. */
  conversationsDeleted?: (conversationIds: string[]) => void
  newId?: () => string
}

type Handler = (ctx: Context) => Promise<Response | null>

interface Context {
  request: Request
  params: string[]
  query: URLSearchParams
  json: <T>() => Promise<T>
}

const ok = (body: unknown, status = 200) => Response.json(body, { status })
const empty = (status = 204) => new Response(null, { status })

interface Route {
  method: string
  pattern: RegExp
  handler: Handler
}

/** The path after the API's base URL, or null for a request that is not to the API. */
export function apiPath(url: string, baseUrl: string, origin: string): string | null {
  const target = new URL(url, origin)
  const base = new URL(baseUrl, origin)
  if (target.origin !== base.origin) return null
  const prefix = base.pathname.replace(/\/$/, '')
  if (target.pathname !== prefix && !target.pathname.startsWith(`${prefix}/`)) return null
  return target.pathname.slice(prefix.length) || '/'
}

export function createGuestApi(deps: GuestApiDeps) {
  const { repo, runner, hub } = deps
  const newId = deps.newId ?? (() => crypto.randomUUID())
  const id = '([^/]+)'
  const compaction = { repo, hub, baseUrl: deps.baseUrl, fetch: deps.fetch, changed: deps.compactionChanged }

  /** The settings a run is asked with, from a request body. */
  const choiceOf = (body: { model?: string; reasoningEffort?: ReasoningEffort; webSearch?: boolean }) => ({
    ...(body.model?.trim() ? { model: body.model.trim() } : {}),
    ...(body.reasoningEffort ? { reasoningEffort: body.reasoningEffort } : {}),
    ...(body.webSearch !== undefined ? { webSearch: body.webSearch } : {}),
  })

  const messageOf = async (conversationId: string, messageId: string): Promise<Message> => {
    const data = await repo.data(conversationId)
    const row = data.messages.find((m) => m.id === messageId)
    if (!row) throw notFound()
    const [message] = await repo.rowsOf([row])
    if (!message) throw notFound()
    return message
  }

  const routes: Route[] = [
    // ---- the page needs no account
    { method: 'GET', pattern: /^\/auth\/session$/, handler: async () => new GuestError(401, 'unauthorized', 'authentication required').toResponse() },

    // ---- what the server offers
    { method: 'GET', pattern: /^\/models$/, handler: async () => ok((await deps.config()).models) },
    { method: 'GET', pattern: /^\/features$/, handler: async () => ok((await deps.config()).features) },

    // ---- conversations
    {
      method: 'GET',
      pattern: /^\/conversations$/,
      handler: async ({ query }) =>
        ok(
          await repo.listConversations({
            limit: query.has('limit') ? Number(query.get('limit')) : undefined,
            cursor: query.get('cursor') || undefined,
            search: query.get('search') || undefined,
          }),
        ),
    },
    { method: 'POST', pattern: /^\/conversations$/, handler: async ({ json }) => ok(await repo.createConversation(await json()), 201) },
    { method: 'GET', pattern: /^\/conversations\/running$/, handler: async () => ok({ ids: await repo.running() }) },
    { method: 'GET', pattern: new RegExp(`^/conversations/${id}$`), handler: async ({ params }) => ok(await repo.getConversation(params[0]!)) },
    {
      method: 'PATCH',
      pattern: new RegExp(`^/conversations/${id}$`),
      handler: async ({ params, json }) => ok(await repo.updateConversation(params[0]!, await json())),
    },
    {
      method: 'DELETE',
      pattern: new RegExp(`^/conversations/${id}$`),
      handler: async ({ params }) => {
        runner.abort(params[0]!)
        await repo.deleteConversation(params[0]!)
        hub.announceDeleted(params[0]!)
        deps.conversationsDeleted?.([params[0]!])
        return empty()
      },
    },
    {
      method: 'POST',
      pattern: new RegExp(`^/conversations/${id}/title$`),
      handler: async ({ params }) => {
        const conversationId = params[0]!
        const data = await repo.data(conversationId)
        if (data.conversation.temporary) throw new GuestError(409, 'temporary_conversation', 'a temporary chat is not titled')
        const exchanges = transcriptOf(data)
          .filter((m) => (m.role === 'user' || m.role === 'assistant') && m.content.trim() !== '')
          .slice(-8)
          .map((m) => ({ role: m.role as 'user' | 'assistant', content: m.content }))
        if (exchanges.length === 0) throw new GuestError(409, 'nothing_to_title', 'this conversation has nothing to title yet')
        const base = deps.baseUrl.replace(/\/$/, '')
        let response: Response
        try {
          response = await deps.fetch(`${base}/guest/title`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ messages: exchanges, ...(data.conversation.model ? { model: data.conversation.model } : {}) }),
          })
        } catch {
          throw new GuestError(502, 'title_failed', 'the model gave no title')
        }
        if (!response.ok) return response
        const { title } = (await response.json()) as { title: string }
        return ok(await repo.updateConversation(conversationId, { title }))
      },
    },
    {
      method: 'GET',
      pattern: new RegExp(`^/conversations/${id}/context$`),
      handler: async ({ params }) => ok(await repo.context(params[0]!)),
    },
    // ---- the compressed context: kept in the page, written by the server's model
    {
      method: 'GET',
      pattern: new RegExp(`^/conversations/${id}/compaction$`),
      handler: async ({ params }) => ok(await repo.compaction(params[0]!)),
    },
    {
      method: 'POST',
      pattern: new RegExp(`^/conversations/${id}/compaction$`),
      handler: async ({ params }) => {
        const summary = await compactGuestConversation(compaction, params[0]!)
        return ok({ summary })
      },
    },
    {
      method: 'DELETE',
      pattern: new RegExp(`^/conversations/${id}/compaction$`),
      handler: async ({ params }) => {
        await undoGuestCompaction(compaction, params[0]!)
        return empty()
      },
    },
    {
      method: 'GET',
      pattern: new RegExp(`^/conversations/${id}/messages$`),
      handler: async ({ params, query }) =>
        ok(
          await repo.listMessages(params[0]!, {
            before: query.get('before') || undefined,
            limit: query.has('limit') ? Number(query.get('limit')) : undefined,
          }),
        ),
    },
    {
      method: 'GET',
      pattern: new RegExp(`^/conversations/${id}/events$`),
      handler: async ({ params, request }) => {
        await repo.getConversation(params[0]!)
        return eventStream(hub, params[0]!, request.signal)
      },
    },

    // ---- asking
    {
      method: 'POST',
      pattern: new RegExp(`^/conversations/${id}/messages$`),
      handler: async ({ params, json }) => {
        const conversationId = params[0]!
        const body = await json<{ content: string; attachmentIds?: string[] } & Parameters<typeof choiceOf>[0]>()
        const content = (body.content ?? '').trim()
        if (!content) throw new GuestError(400, 'empty_content', 'content must not be empty')
        await repo.getConversation(conversationId)
        const started = await runner.start(
          conversationId,
          { action: 'send', content, attachmentIds: body.attachmentIds ?? [], id: newId() },
          choiceOf(body),
        )
        return ok({ message: await messageOf(conversationId, started.inputId), run: started.run }, 202)
      },
    },
    {
      method: 'POST',
      pattern: new RegExp(`^/conversations/${id}/messages/${id}/edit$`),
      handler: async ({ params, json }) => {
        const [conversationId, messageId] = [params[0]!, params[1]!]
        const body = await json<{ content: string } & Parameters<typeof choiceOf>[0]>()
        const content = (body.content ?? '').trim()
        if (!content) throw new GuestError(400, 'empty_content', 'content must not be empty')
        await repo.getConversation(conversationId)
        const started = await runner.start(conversationId, { action: 'edit', content, messageId, id: newId() }, choiceOf(body))
        return ok({ message: await messageOf(conversationId, started.inputId), run: started.run }, 202)
      },
    },
    {
      method: 'POST',
      pattern: new RegExp(`^/conversations/${id}/regenerate$`),
      handler: async ({ params, json }) => {
        const conversationId = params[0]!
        await repo.getConversation(conversationId)
        const started = await runner.start(conversationId, { action: 'regenerate' }, choiceOf(await json()))
        return ok({ message: await messageOf(conversationId, started.inputId), run: started.run }, 202)
      },
    },
    {
      method: 'POST',
      pattern: new RegExp(`^/conversations/${id}/tool-calls/${id}/answer$`),
      handler: async ({ params, json }) => {
        const [conversationId, callId] = [params[0]!, decodeURIComponent(params[1]!)]
        const body = await json<{ answers: { selected: string[]; other?: string }[] } & Parameters<typeof choiceOf>[0]>()
        await repo.getConversation(conversationId)
        const started = await runner.start(conversationId, { action: 'answer', callId, answers: body.answers ?? [] }, choiceOf(body))
        const result = started.written.find((m) => m.role === 'tool')
        if (!result) throw new GuestError(502, 'guest_unreachable', 'the server did not record the answer')
        return ok({ message: await messageOf(conversationId, started.inputId), result, run: started.run }, 202)
      },
    },
    {
      method: 'POST',
      pattern: new RegExp(`^/conversations/${id}/runs/${id}/cancel$`),
      handler: async ({ params }) => {
        await repo.getConversation(params[0]!)
        // A run whose stream this page does not hold (it was reloaded) is gone with it.
        if (runner.isRunning(params[0]!)) await runner.cancel(params[1]!).catch(() => undefined)
        return empty(202)
      },
    },

    // ---- the person's own
    { method: 'GET', pattern: /^\/me\/personalization$/, handler: async () => ok(await repo.personalization()) },
    { method: 'PATCH', pattern: /^\/me\/personalization$/, handler: async ({ json }) => ok(await repo.updatePersonalization(await json())) },
    { method: 'GET', pattern: /^\/me\/memories$/, handler: async () => ok({ items: await repo.memories() }) },
    { method: 'POST', pattern: /^\/me\/memories$/, handler: async ({ json }) => ok(await repo.createMemory((await json<{ content: string }>()).content ?? ''), 201) },
    {
      method: 'DELETE',
      pattern: /^\/me\/memories$/,
      handler: async () => {
        await repo.clearMemories()
        return empty()
      },
    },
    {
      method: 'PATCH',
      pattern: new RegExp(`^/me/memories/${id}$`),
      handler: async ({ params, json }) => ok(await repo.updateMemory(params[0]!, (await json<{ content: string }>()).content ?? '')),
    },
    {
      method: 'DELETE',
      pattern: new RegExp(`^/me/memories/${id}$`),
      handler: async ({ params }) => {
        await repo.deleteMemory(params[0]!)
        return empty()
      },
    },
    { method: 'GET', pattern: /^\/me\/memory-events$/, handler: async () => ok({ items: await repo.memoryEvents() }) },
    {
      method: 'POST',
      pattern: new RegExp(`^/me/memory-events/${id}/undo$`),
      handler: async ({ params }) => ok(await repo.undoMemoryEvent(params[0]!)),
    },
    {
      method: 'GET',
      pattern: /^\/me\/export$/,
      handler: async () => {
        const body = await repo.export()
        return new Response(JSON.stringify(body), {
          headers: {
            'Content-Type': 'application/json',
            'Content-Disposition': `attachment; filename="openchat-export-${body.exportedAt.slice(0, 10)}.json"`,
          },
        })
      },
    },
    {
      method: 'DELETE',
      pattern: /^\/me\/conversations$/,
      handler: async () => {
        const ids = (await repo.storage.conversations()).map((c) => c.id)
        for (const id of ids) runner.abort(id)
        const deleted = await repo.deleteAll()
        for (const id of ids) hub.announceDeleted(id)
        deps.conversationsDeleted?.(ids)
        return ok({ deleted })
      },
    },

    // ---- files: kept in the page, sent with the message that carries them
    {
      method: 'POST',
      pattern: /^\/files$/,
      handler: async ({ json }) => {
        const body = await json<{ filename: string; contentType: string; size: number }>()
        const limit = UPLOADABLE[(body.contentType ?? '').split(';')[0]!.trim().toLowerCase()]
        if (!body.filename?.trim()) throw new GuestError(400, 'invalid_filename', 'filename must not be empty')
        if (!limit) throw new GuestError(415, 'unsupported_file_type', 'only images (PNG, JPEG, WebP, GIF) and PDFs can be attached')
        if (!(body.size > 0)) throw new GuestError(400, 'invalid_size', 'size must be positive')
        if (body.size > limit) throw new GuestError(413, 'file_too_large', `a file can be at most ${limit >> 20} MiB`)
        const fileId = newId()
        return ok(
          { id: fileId, uploadUrl: `${LOCAL_UPLOAD}${fileId}`, url: '', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString() },
          201,
        )
      },
    },
  ]

  return {
    /** Answers a request for `path` (after the API's base URL), or null if the server is to. */
    async handle(request: Request, path: string): Promise<Response | null> {
      const url = new URL(request.url, 'http://local.invalid')
      for (const route of routes) {
        if (route.method !== request.method) continue
        const match = route.pattern.exec(path)
        if (!match) continue
        try {
          return await route.handler({
            request,
            params: match.slice(1),
            query: url.searchParams,
            json: async <T,>() => {
              try {
                return (await request.json()) as T
              } catch {
                throw new GuestError(400, 'invalid_body', 'request body is not valid JSON')
              }
            },
          })
        } catch (error) {
          if (error instanceof GuestError) return error.toResponse()
          console.error('guest api', error)
          return new GuestError(500, 'internal_error', 'internal error').toResponse()
        }
      }
      return null
    },
  }
}

/** The files a guest can attach, with the largest of each (the server's `uploadable`). */
const UPLOADABLE: Record<string, number> = {
  'image/png': 5 << 20,
  'image/jpeg': 5 << 20,
  'image/webp': 5 << 20,
  'image/gif': 5 << 20,
  'application/pdf': 10 << 20,
}

/** Where `POST /files` says to put a file: not an address, a handle the upload code keeps the file under. */
export const LOCAL_UPLOAD = 'guest-local://upload/'
