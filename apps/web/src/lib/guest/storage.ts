import type {
  Memory,
  MemoryEvent,
  StoredBlob,
  StoredConversation,
  StoredMessage,
  StoredPersonalization,
  StoredRun,
} from './types'

/**
 * Where a guest's data is kept. The repository (repo.ts) decides what to read
 * and write; this is only the reading and writing, so the same logic runs over
 * IndexedDB in the browser (idb.ts) and over maps in the tests.
 *
 * Nothing here is atomic across calls. The repository serializes the work of
 * one conversation, and a page that dies between two writes leaves a state the
 * repair at load time (repo.repair) can read.
 */
export interface GuestStorage {
  conversations(): Promise<StoredConversation[]>
  conversation(id: string): Promise<StoredConversation | undefined>
  putConversation(conversation: StoredConversation): Promise<void>
  /** Removes the conversation with its messages, runs and files. */
  deleteConversation(id: string): Promise<void>

  messages(conversationId: string): Promise<StoredMessage[]>
  putMessages(messages: StoredMessage[]): Promise<void>

  runs(conversationId: string): Promise<StoredRun[]>
  allRuns(): Promise<StoredRun[]>
  putRuns(runs: StoredRun[]): Promise<void>

  blob(id: string): Promise<StoredBlob | undefined>
  putBlob(blob: StoredBlob): Promise<void>
  deleteBlob(id: string): Promise<void>
  /** Files whose message was never sent; the repository sweeps them. */
  blobsOf(conversationId: string): Promise<StoredBlob[]>

  memories(): Promise<Memory[]>
  putMemory(memory: Memory): Promise<void>
  deleteMemory(id: string): Promise<void>
  clearMemories(): Promise<void>

  memoryEvents(): Promise<MemoryEvent[]>
  putMemoryEvent(event: MemoryEvent): Promise<void>
  deleteMemoryEvents(filter: (event: MemoryEvent) => boolean): Promise<void>

  personalization(): Promise<StoredPersonalization | undefined>
  putPersonalization(value: StoredPersonalization): Promise<void>

  /** Everything, for "delete all my data". */
  clear(): Promise<void>
}

/** A storage in maps. For the tests, and for a browser that will not give IndexedDB. */
export function memoryStorage(): GuestStorage {
  const conversations = new Map<string, StoredConversation>()
  const messages = new Map<string, StoredMessage>()
  const runs = new Map<string, StoredRun>()
  const blobs = new Map<string, StoredBlob>()
  const memories = new Map<string, Memory>()
  const events = new Map<string, MemoryEvent>()
  let personalization: StoredPersonalization | undefined

  const copy = <T,>(value: T): T => structuredClone(value)
  const of = <T extends { conversationId: string }>(map: Map<string, T>, id: string) =>
    [...map.values()].filter((row) => row.conversationId === id)

  return {
    conversations: async () => [...conversations.values()].map(copy),
    conversation: async (id) => {
      const found = conversations.get(id)
      return found && copy(found)
    },
    putConversation: async (c) => void conversations.set(c.id, copy(c)),
    deleteConversation: async (id) => {
      conversations.delete(id)
      for (const map of [messages, runs, blobs] as Map<string, { conversationId: string }>[]) {
        for (const [key, row] of map) if (row.conversationId === id) map.delete(key)
      }
    },

    messages: async (id) => of(messages, id).map(copy),
    putMessages: async (rows) => rows.forEach((m) => messages.set(m.id, copy(m))),

    runs: async (id) => of(runs, id).map(copy),
    allRuns: async () => [...runs.values()].map(copy),
    putRuns: async (rows) => rows.forEach((r) => runs.set(r.id, copy(r))),

    blob: async (id) => blobs.get(id),
    putBlob: async (b) => void blobs.set(b.id, b),
    deleteBlob: async (id) => void blobs.delete(id),
    blobsOf: async (id) => of(blobs, id),

    memories: async () => [...memories.values()].map(copy),
    putMemory: async (m) => void memories.set(m.id, copy(m)),
    deleteMemory: async (id) => void memories.delete(id),
    clearMemories: async () => memories.clear(),

    memoryEvents: async () => [...events.values()].map(copy),
    putMemoryEvent: async (e) => void events.set(e.id, copy(e)),
    deleteMemoryEvents: async (filter) => {
      for (const [key, event] of events) if (filter(event)) events.delete(key)
    },

    personalization: async () => personalization && copy(personalization),
    putPersonalization: async (value) => {
      personalization = copy(value)
    },

    clear: async () => {
      for (const map of [conversations, messages, runs, blobs, memories, events] as Map<string, unknown>[]) map.clear()
      personalization = undefined
    },
  }
}
