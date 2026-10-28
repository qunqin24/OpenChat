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
    /** Its sub-agents: what the model wrote, so never persisted. */
    subagents: (id: string) => [...queryKeys.conversations.all, 'subagents', id] as const,
    subagent: (id: string, subagentId: string) => [...queryKeys.conversations.all, 'subagent', id, subagentId] as const,
  },
  messages: {
    all: ['messages'] as const,
    list: (conversationId: string) => [...queryKeys.messages.all, 'list', conversationId] as const,
  },
  /** The library: signed URLs that expire, so never persisted. */
  library: {
    all: ['library'] as const,
    list: (search: string, type: string) => [...queryKeys.library.all, 'list', search, type] as const,
  },
  /** Scheduled tasks: small, and moved on by the server, so never persisted. */
  tasks: {
    all: ['tasks'] as const,
    list: () => [...queryKeys.tasks.all, 'list'] as const,
  },
  /** An administrator's 轨迹 of one conversation: debugging data, never persisted. */
  trace: (conversationId: string) => ['trace', conversationId] as const,
  /** A writing block with its versions: the person's text, never persisted. */
  document: (id: string) => ['document', id] as const,
  /** A conversation's writing blocks, to reopen one (DocumentsMenu). */
  conversationDocuments: (conversationId: string) => ['conversation-documents', conversationId] as const,
  features: ['features'] as const,
  auth: {
    /** Who is signed in; `null` when nobody is. */
    session: ['auth', 'session'] as const,
    /** What the sign-in pages offer: registration (public). */
    options: ['auth', 'options'] as const,
    /** The signed-in account's passkeys. */
    passkeys: ['auth', 'passkeys'] as const,
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
