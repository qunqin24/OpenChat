import { create } from 'zustand'

import { useDocumentPanel } from './document-panel-store'

interface SubagentPanelState {
  /** The conversation whose sub-agents the panel lists; null when it is closed. */
  conversationId: string | null
  /** The sub-agent whose work the panel shows; null lists them all. */
  selected: string | null
  /** The corner summary stays up, once the panel was opened, until it is closed. */
  seen: string | null
  open: (conversationId: string, selected?: string | null) => void
  select: (selected: string | null) => void
  close: () => void
}

/**
 * The side panel of a conversation's sub-agents (SubagentPanel): open or not,
 * and which sub-agent it shows. It takes the writing block's place beside the
 * chat, so opening one closes the other.
 */
export const useSubagentPanel = create<SubagentPanelState>()((set) => ({
  conversationId: null,
  selected: null,
  seen: null,
  open: (conversationId, selected = null) => {
    useDocumentPanel.getState().close()
    set({ conversationId, selected, seen: conversationId })
  },
  select: (selected) => set({ selected }),
  close: () => set({ conversationId: null, selected: null, seen: null }),
}))

// A writing block opened puts the sub-agents away (the other half of the swap).
useDocumentPanel.subscribe((state, previous) => {
  if (state.documentId !== null && previous.documentId === null && useSubagentPanel.getState().conversationId !== null) {
    useSubagentPanel.setState({ conversationId: null, selected: null })
  }
})
