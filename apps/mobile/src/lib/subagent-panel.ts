import { create } from 'zustand'

/** The sub-agents' sheet: open for a conversation, at one sub-agent's work (`selected`) or the list. */
export const useSubagentPanel = create<{
  conversationId: string | null
  selected: string | null
  open: (conversationId: string, selected?: string | null) => void
  select: (selected: string | null) => void
  close: () => void
}>()((set) => ({
  conversationId: null,
  selected: null,
  open: (conversationId, selected = null) => set({ conversationId, selected }),
  select: (selected) => set({ selected }),
  close: () => set({ conversationId: null, selected: null }),
}))
