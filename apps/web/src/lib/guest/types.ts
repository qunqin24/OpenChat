import type { Attachment, Conversation, Memory, MemoryEvent, Message, Run } from '@openchat/api-client'

/**
 * What a guest's browser keeps, in IndexedDB. The server keeps nothing of a
 * guest, so the shapes here are the ones it would have kept - rows of messages
 * and runs, a conversation pointing at the end of the branch shown - and the
 * modules of this directory reproduce what its queries do with them
 * (conversation_path, conversation_transcript, ListVersions, PickBranchLeaf,
 * ChooseAnswer). The contract's own types are what leave the module: the rest
 * of the page reads the same `Message` and `Conversation` as for an account.
 */

/** A file a message carries; its bytes are in the blob store, under the same id. */
export type StoredAttachment = Omit<Attachment, 'url'>

/** A transcript row as kept: the contract's `Message`, less what is worked out when it is read. */
export interface StoredMessage extends Omit<Message, 'attachments' | 'versions' | 'branches'> {
  attachments?: StoredAttachment[]
  /** User messages: the question this one follows; null for a conversation's first. */
  parentInputId?: string | null
  /** User messages: the last time this was the end of the branch shown. */
  shownAt?: number | null
}

/** A run as kept, with the question it answers and whether a regeneration set it aside. */
export interface StoredRun extends Run {
  inputId: string
  superseded: boolean
  /** The page that is following it, and when it last said so: a run whose page went away is repaired. */
  tab?: string
  heartbeatAt?: number
}

export interface StoredConversation extends Conversation {
  /** The end of the branch shown: a user message with no children. */
  currentInputId: string | null
  /** What the system prompt was rendered from at the first run (the server's memory.Snapshot). */
  memorySnapshot: Record<string, unknown> | null
}

/** One conversation as the tree functions read it. */
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
