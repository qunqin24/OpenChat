/**
 * One place for every query key, so invalidation never has to guess at a shape.
 */
export const queryKeys = {
  health: ['health'] as const,
  conversations: {
    all: ['conversations'] as const,
    list: () => [...queryKeys.conversations.all, 'list'] as const,
    /** Kept apart from `list`, which is persisted: search results quote messages. */
    /** Not persisted: it is only ever true for a moment. */
    running: () => [...queryKeys.conversations.all, 'running'] as const,
    search: (term: string) => [...queryKeys.conversations.all, 'search', term] as const,
    detail: (id: string) => [...queryKeys.conversations.all, 'detail', id] as const,
    context: (id: string) => [...queryKeys.conversations.all, 'context', id] as const,
    /** The compressed context: summary text of the conversation, so never persisted. */
    compaction: (id: string) => [...queryKeys.conversations.all, 'compaction', id] as const,
  },
  messages: {
    all: ['messages'] as const,
    list: (conversationId: string) => [...queryKeys.messages.all, 'list', conversationId] as const,
  },
  features: ['features'] as const,
  guest: {
    status: ['guest', 'status'] as const,
  },
  auth: {
    /** Who is signed in; `null` when nobody is. */
    session: ['auth', 'session'] as const,
    /** What the sign-in pages offer: registration, guest mode (public). */
    options: ['auth', 'options'] as const,
  },
  memory: {
    all: ['memory'] as const,
    personalization: () => [...queryKeys.memory.all, 'personalization'] as const,
    list: () => [...queryKeys.memory.all, 'list'] as const,
    events: () => [...queryKeys.memory.all, 'events'] as const,
  },
  models: {
    all: ['models'] as const,
    list: () => [...queryKeys.models.all, 'list'] as const,
  },
} as const
