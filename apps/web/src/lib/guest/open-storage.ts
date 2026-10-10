import { idbStorage } from './idb'
import { memoryStorage, type GuestStorage } from './storage'

/**
 * Chooses where a guest's conversations are kept, once, when guest mode starts.
 *
 * IndexedDB is the place. But a browser can have it and still refuse it: some
 * private modes, blocked site data, a profile out of space. Finding that out on
 * the first write would leave the guest with a chat that fails on every message,
 * so it is tried first, and when it cannot be opened the conversations live in
 * memory instead - the chat works, and says (`kept: false`) that nothing will
 * survive a reload.
 */
export interface OpenedStorage {
  storage: GuestStorage
  /** Whether what is written will still be there after a reload. */
  kept: boolean
}

export async function openGuestStorage(
  indexedDbStorage: typeof idbStorage = idbStorage,
  hasIndexedDb: boolean = typeof indexedDB !== 'undefined',
): Promise<OpenedStorage> {
  if (hasIndexedDb) {
    try {
      const storage = indexedDbStorage()
      await storage.probe()
      return { storage, kept: true }
    } catch {
      // Present, but it cannot be opened.
    }
  }
  return { storage: memoryStorage(), kept: false }
}
