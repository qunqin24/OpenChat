/**
 * One place for every query key, so invalidation never has to guess at a shape.
 */
export const queryKeys = {
  health: ['health'] as const,
  conversations: {
    all: ['conversations'] as const,
    list: () => [...queryKeys.conversations.all, 'list'] as const,
    /** Kept apart from `list`, which is persisted: search results quote messages. */
    search: (term: string) => [...queryKeys.conversations.all, 'search', term] as const,
    detail: (id: string) => [...queryKeys.conversations.all, 'detail', id] as const,
    context: (id: string) => [...queryKeys.conversations.all, 'context', id] as const,
  },
  messages: {
    all: ['messages'] as const,
    list: (conversationId: string) => [...queryKeys.messages.all, 'list', conversationId] as const,
  },
  features: ['features'] as const,
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
