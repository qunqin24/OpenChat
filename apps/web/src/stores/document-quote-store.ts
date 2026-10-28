import { create } from 'zustand'

import type { DocumentQuote } from '@/lib/document-selection'

interface DocumentQuoteState {
  /** A passage waiting in a conversation's composer for the person's request. */
  quote: (DocumentQuote & { conversationId: string }) | null
  set: (quote: (DocumentQuote & { conversationId: string }) | null) => void
}

/**
 * The passage "提出修改请求" put in the composer (DocumentPanel → Composer):
 * sent with the next message of its conversation, then cleared.
 */
export const useDocumentQuote = create<DocumentQuoteState>()((set) => ({
  quote: null,
  set: (quote) => set({ quote }),
}))
