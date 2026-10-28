import { create } from 'zustand'

import type { PendingEdit } from '@/lib/document-tool'

interface DocumentActivityState {
  /**
   * The edit a reply is writing now, by the card showing it: what the panel
   * marks. By card, not by tool call: the streaming reply and the stored
   * message each draw the call for a moment, and the one leaving must not
   * take the other's entry with it.
   */
  edits: Record<string, PendingEdit>
  put: (card: string, edit: PendingEdit) => void
  drop: (card: string) => void
}

/**
 * Edits in flight, from the reply being written (DocumentCard) to the side
 * panel (DocumentPanel), which shows the passages being rewritten as
 * placeholders until the new version arrives.
 */
export const useDocumentActivity = create<DocumentActivityState>()((set) => ({
  edits: {},
  put: (card, edit) =>
    set((state) => {
      const held = state.edits[card]
      if (held && held.documentId === edit.documentId && held.rewrite === edit.rewrite && held.finds.join('\u0000') === edit.finds.join('\u0000')) return state
      return { edits: { ...state.edits, [card]: edit } }
    }),
  drop: (card) =>
    set((state) => {
      if (!(card in state.edits)) return state
      const rest = { ...state.edits }
      delete rest[card]
      return { edits: rest }
    }),
}))
