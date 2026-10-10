import type { Attachment, ContextSummary, Conversation, Memory, MemoryEvent, Message, Run } from '@openchat/api-client'

/**
 * What a guest's browser keeps, in IndexedDB. The server keeps nothing of a
 * guest, so the shapes here are the ones it would have kept - rows of messages
 * and runs, in a conversation that is its rows in the order they were written -
 * and the modules of this directory reproduce what its queries do with them.
 * The contract's own types are what leave the module: the rest of the page
 * reads the same `Message` and `Conversation` as for an account.
 */

/** A file a message carries; its bytes are in the blob store, under the same id. */
export type StoredAttachment = Omit<Attachment, 'url'>

/** A transcript row as kept: the contract's `Message`, less what is worked out when it is read. */
export interface StoredMessage extends Omit<Message, 'attachments'> {
  attachments?: StoredAttachment[]
}

/** A run as kept, with the question it answers. */
export interface StoredRun extends Run {
  inputId: string
  /** The page that is following it, and when it last said so: a run whose page went away is repaired. */
  tab?: string
  heartbeatAt?: number
}

export interface StoredConversation extends Conversation {
  /** What the system prompt was rendered from at the first run (the server's memory.Snapshot). */
  memorySnapshot: Record<string, unknown> | null
  /**
   * The conversation's compressed context (summary.ts). Kept with the
   * conversation, and valid only while the rows it names are there: read it
   * through `survivingSummary`.
   */
  summary?: ContextSummary | null
}

/** One conversation as the functions over it read it. */
export interface ConversationData {
  conversation: StoredConversation
  messages: StoredMessage[]
  runs: StoredRun[]
}

export interface StoredBlob {
  id: string
  conversationId: string
  blob: Blob
  filename: string
  contentType: string
  size: number
  /** When it was picked. A file no message ever claimed (conversationId '') is swept after a day. */
  at?: number
}

export interface StoredPersonalization {
  aboutYou: string
  responseStyle: string
  memoryEnabled: boolean
}

export type { Memory, MemoryEvent }
