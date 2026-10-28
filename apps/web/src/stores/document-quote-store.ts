import { create } from 'zustand'

import { MAX_QUOTES, type DocumentQuote } from '@/lib/document-selection'

export type ComposerQuote = DocumentQuote & { conversationId: string }

interface DocumentQuoteState {
  /**
   * The passages waiting in a conversation's composer for the person's
   * request, all of one conversation: one passage of a writing block, or up
   * to MAX_QUOTES passages of answers.
   */
  quotes: ComposerQuote[]
  /** A writing block's passage ("提出修改请求"): it alone, replacing what was there. */
  set: (quote: ComposerQuote | null) => void
  /** A passage of an answer ("引用提问"): added to the answers' passages already there. */
  add: (quote: ComposerQuote) => void
  remove: (index: number) => void
}

/**
 * The passages quoted into the composer (DocumentPanel, AskSelection →
 * Composer): sent with the next message of their conversation, then cleared.
 */
export const useDocumentQuote = create<DocumentQuoteState>()((set) => ({
  quotes: [],
  set: (quote) => set({ quotes: quote ? [quote] : [] }),
  add: (quote) =>
    set(({ quotes }) => {
      // Another conversation's, or a writing block's request, gives way.
      const kept = quotes.filter((q) => q.conversationId === quote.conversationId && !q.documentId && q.passage !== quote.passage)
      return { quotes: [...kept, quote].slice(-MAX_QUOTES) }
    }),
  remove: (index) => set(({ quotes }) => ({ quotes: quotes.filter((_, i) => i !== index) })),
}))
