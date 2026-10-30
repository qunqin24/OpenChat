import { create } from 'zustand'

import { MAX_QUOTES, type DocumentQuote } from '@openchat/chat-core/document-selection'

/** The writing block open full screen, and the version shown (null: the latest, which is editable). */
export const useDocumentScreen = create<{
  documentId: string | null
  viewing: number | null
  open: (documentId: string, viewing?: number | null) => void
  view: (viewing: number | null) => void
  close: () => void
}>()((set) => ({
  documentId: null,
  viewing: null,
  open: (documentId, viewing = null) => set({ documentId, viewing }),
  view: (viewing) => set({ viewing }),
  close: () => set({ documentId: null, viewing: null }),
}))

export type ComposerQuote = DocumentQuote & { conversationId: string }

/**
 * Passages quoted into a conversation's composer (the web's
 * document-quote-store): a writing block's passage ("提出修改请求") alone, sent
 * with the next message and then cleared.
 */
export const useDocumentQuote = create<{ quotes: ComposerQuote[]; set: (quote: ComposerQuote | null) => void; remove: (index: number) => void }>()(
  (set) => ({
    quotes: [],
    set: (quote) => set({ quotes: quote ? [quote].slice(-MAX_QUOTES) : [] }),
    remove: (index) => set(({ quotes }) => ({ quotes: quotes.filter((_, i) => i !== index) })),
  }),
)
