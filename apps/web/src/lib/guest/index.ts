import type { GuestConfig } from '@openchat/api-client'

import { API_BASE_URL } from '@/lib/api-base'
import { queryClient } from '@/lib/query'
import { queryKeys } from '@/lib/query-keys'
import { idbStorage } from './idb'
import { GuestHub } from './hub'
import { isGuestMode } from './mode'
import { GuestRepo, type PersonalizationLimits } from './repo'
import { createGuestApi, apiPath } from './router'
import { GuestRunner } from './runner'
import { memoryStorage } from './storage'

export { isGuestMode, syncMode, useIsGuest } from './mode'

/**
 * The guest's side of the page, put together once: the storage, the repository
 * over it, the hub that makes the events, the runner that asks the server, and
 * the API that answers the page's calls from them. Built the first time a guest
 * page makes a call, so an account never loads any of it.
 */
interface Wired {
  repo: GuestRepo
  hub: GuestHub
  runner: GuestRunner
  api: ReturnType<typeof createGuestApi>
}

let wired: Wired | undefined
let config: { at: number; value: GuestConfig } | undefined
let configLoad: Promise<GuestConfig> | undefined

/** How long what the server offers is kept before it is asked again. */
const CONFIG_TTL_MS = 30_000

/** Every request to the network goes through here, never through the page's own fetch (which would serve it locally). */
const network: typeof globalThis.fetch = (input, init) => globalThis.fetch(input, init)

async function loadConfig(): Promise<GuestConfig> {
  if (config && Date.now() - config.at < CONFIG_TTL_MS) return config.value
  configLoad ??= network(`${API_BASE_URL.replace(/\/$/, '')}/guest/config`)
    .then(async (response) => {
      if (!response.ok) throw new Error(`guest config: ${response.status}`)
      const value = (await response.json()) as GuestConfig
      config = { at: Date.now(), value }
      return value
    })
    .finally(() => {
      configLoad = undefined
    })
  return configLoad
}

const limits = (): PersonalizationLimits =>
  config?.value.personalization ?? { memoryAllowed: true, instructionLimit: 1500, memoryLimit: 100, memoryLength: 200 }

function wire(): Wired {
  if (wired) return wired
  const storage = typeof indexedDB === 'undefined' ? memoryStorage() : idbStorage()
  const repo = new GuestRepo(storage, { limits })
  const hub = new GuestHub()
  const channel = typeof BroadcastChannel === 'undefined' ? undefined : new BroadcastChannel('openchat-guest')

  const refreshLists = () => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.list() })
    void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.running() })
  }
  const runner = new GuestRunner({
    repo,
    hub,
    baseUrl: API_BASE_URL,
    fetch: network,
    tab: crypto.randomUUID(),
    onSettled: (conversationId) => {
      // The memory tool may have changed the list, and the conversation has its title.
      void queryClient.invalidateQueries({ queryKey: queryKeys.memory.all })
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations.context(conversationId) })
      refreshLists()
      channel?.postMessage({ conversationId })
    },
    onDeleted: (conversationId, from) => channel?.postMessage({ conversationId, from }),
  })
  const api = createGuestApi({
    repo,
    runner,
    hub,
    baseUrl: API_BASE_URL,
    fetch: network,
    config: loadConfig,
    // The summary is read again with the transcript, so this tells it as a change.
    compactionChanged: (conversationId) => channel?.postMessage({ conversationId }),
  })
  // Another tab of the same guest changed a conversation (a run finished, or
  // started over a deletion of rows).
  if (channel) {
    channel.onmessage = (event: MessageEvent<{ conversationId?: string; from?: string }>) => {
      const id = event.data?.conversationId
      // A run this tab is following reports itself.
      if (id && !runner.isRunning(id)) hub.announceChanged(id, event.data?.from)
      refreshLists()
    }
  }
  wired = { repo, hub, runner, api }
  return wired
}

/**
 * A guest's calls: to the guest API when it serves them (conversations,
 * messages, memories, the person's data, uploads) and to the network when it
 * does not.
 */
export const guestFetch: typeof globalThis.fetch = async (input, init) => {
  if (!isGuestMode()) return network(input, init)
  const request = new Request(input, init)
  const path = apiPath(request.url, API_BASE_URL, window.location.origin)
  if (path !== null) {
    const answered = await wire().api.handle(request, path)
    if (answered) return answered
  }
  return network(request)
}

/** A file picked for the next message; kept in the page until a message claims it. */
export async function putPendingFile(id: string, file: File, contentType: string): Promise<void> {
  await wire().repo.putFile({ id, conversationId: '', blob: file, filename: file.name, contentType, size: file.size })
}

/** What the server offers a guest, read ahead of the pages that need it. */
export function prefetchGuestConfig(): Promise<unknown> {
  return loadConfig().catch(() => undefined)
}
