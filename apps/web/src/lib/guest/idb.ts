import type { GuestStorage } from './storage'
import type { StoredBlob, StoredConversation, StoredMessage, StoredPersonalization, StoredRun } from './types'
import type { Memory, MemoryEvent } from './types'

/**
 * The guest's storage in IndexedDB, in one database of this origin. It holds
 * the conversations, their messages and runs, the files messages carry (as
 * blobs), the memories and what the model changed in them, and the one record
 * of custom instructions. A guest who clears the browser's site data has
 * cleared all of it - that is the promise of guest mode, and the notice says so.
 */

const NAME = 'openchat-guest'
const VERSION = 1

const STORES = ['conversations', 'messages', 'runs', 'blobs', 'memories', 'memoryEvents', 'kv'] as const
type Store = (typeof STORES)[number]

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(NAME, VERSION)
    request.onupgradeneeded = () => {
      const db = request.result
      for (const name of STORES) {
        if (db.objectStoreNames.contains(name)) continue
        const store = db.createObjectStore(name, { keyPath: name === 'kv' ? 'key' : 'id' })
        if (name === 'messages' || name === 'runs' || name === 'blobs') store.createIndex('conversationId', 'conversationId')
      }
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error ?? new Error('indexedDB: open failed'))
    request.onblocked = () => reject(new Error('indexedDB: open blocked'))
  })
}

function wrap<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error ?? new Error('indexedDB: request failed'))
  })
}

function finished(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error ?? new Error('indexedDB: transaction failed'))
    tx.onabort = () => reject(tx.error ?? new Error('indexedDB: transaction aborted'))
  })
}

export function idbStorage(): GuestStorage {
  let db: Promise<IDBDatabase> | undefined
  const connect = () => (db ??= open())

  const all = async <T,>(store: Store): Promise<T[]> => {
    const d = await connect()
    return wrap<T[]>(d.transaction(store).objectStore(store).getAll())
  }
  const byConversation = async <T,>(store: Store, id: string): Promise<T[]> => {
    const d = await connect()
    return wrap<T[]>(d.transaction(store).objectStore(store).index('conversationId').getAll(id))
  }
  const get = async <T,>(store: Store, key: string): Promise<T | undefined> => {
    const d = await connect()
    return wrap<T | undefined>(d.transaction(store).objectStore(store).get(key))
  }
  const put = async (store: Store, rows: unknown[]) => {
    if (rows.length === 0) return
    const d = await connect()
    const tx = d.transaction(store, 'readwrite')
    for (const row of rows) tx.objectStore(store).put(row)
    await finished(tx)
  }
  const remove = async (store: Store, key: string) => removeMany(store, [key])
  const removeMany = async (store: Store, keys: string[]) => {
    if (keys.length === 0) return
    const d = await connect()
    const tx = d.transaction(store, 'readwrite')
    for (const key of keys) tx.objectStore(store).delete(key)
    await finished(tx)
  }

  return {
    conversations: () => all<StoredConversation>('conversations'),
    conversation: (id) => get<StoredConversation>('conversations', id),
    putConversation: (c) => put('conversations', [c]),
    deleteConversation: async (id) => {
      const d = await connect()
      const tx = d.transaction(['conversations', 'messages', 'runs', 'blobs'], 'readwrite')
      tx.objectStore('conversations').delete(id)
      for (const name of ['messages', 'runs', 'blobs'] as const) {
        const store = tx.objectStore(name)
        const cursor = store.index('conversationId').openKeyCursor(IDBKeyRange.only(id))
        cursor.onsuccess = () => {
          const at = cursor.result
          if (!at) return
          store.delete(at.primaryKey)
          at.continue()
        }
      }
      await finished(tx)
    },

    messages: (id) => byConversation<StoredMessage>('messages', id),
    putMessages: (rows) => put('messages', rows),
    deleteMessages: (ids) => removeMany('messages', ids),

    runs: (id) => byConversation<StoredRun>('runs', id),
    allRuns: () => all<StoredRun>('runs'),
    putRuns: (rows) => put('runs', rows),
    deleteRuns: (ids) => removeMany('runs', ids),

    blob: (id) => get<StoredBlob>('blobs', id),
    putBlob: (b) => put('blobs', [b]),
    deleteBlob: (id) => remove('blobs', id),
    blobsOf: (id) => byConversation<StoredBlob>('blobs', id),

    memories: () => all<Memory>('memories'),
    putMemory: (m) => put('memories', [m]),
    deleteMemory: (id) => remove('memories', id),
    clearMemories: async () => {
      const d = await connect()
      const tx = d.transaction('memories', 'readwrite')
      tx.objectStore('memories').clear()
      await finished(tx)
    },

    memoryEvents: () => all<MemoryEvent>('memoryEvents'),
    putMemoryEvent: (e) => put('memoryEvents', [e]),
    deleteMemoryEvents: async (filter) => {
      const events = await all<MemoryEvent>('memoryEvents')
      const d = await connect()
      const tx = d.transaction('memoryEvents', 'readwrite')
      for (const event of events) if (filter(event)) tx.objectStore('memoryEvents').delete(event.id)
      await finished(tx)
    },

    personalization: async () => (await get<{ key: string; value: StoredPersonalization }>('kv', 'personalization'))?.value,
    putPersonalization: (value) => put('kv', [{ key: 'personalization', value }]),

    clear: async () => {
      const d = await connect()
      const tx = d.transaction([...STORES], 'readwrite')
      for (const name of STORES) tx.objectStore(name).clear()
      await finished(tx)
    },
  }
}
