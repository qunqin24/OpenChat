import { create } from 'zustand'

interface DocumentPanelState {
  /** The writing block open beside the chat, and the conversation it is in. */
  documentId: string | null
  conversationId: string | null
  /** The version shown when stepping back; null is the latest, which is editable. */
  viewing: number | null
  /** The panel over the whole page rather than beside the chat. */
  full: boolean
  /** Sliding away: still drawn, the chat already back to its width. */
  closing: boolean
  open: (conversationId: string, documentId: string, viewing?: number | null) => void
  view: (viewing: number | null) => void
  close: () => void
  setFull: (full: boolean) => void
}

/**
 * Which writing block the side panel shows (DocumentPanel), and which of its
 * versions: the entries in the replies open it at their own. Kept with its
 * conversation: the chat draws the panel only for its own conversation's
 * document, so opening another conversation leaves it behind.
 */
/** How long the panel takes to slide away (n-doc-panel-out in document.css). */
const PANEL_CLOSE_MS = 200

export const useDocumentPanel = create<DocumentPanelState>()((set, get) => ({
  documentId: null,
  conversationId: null,
  viewing: null,
  full: false,
  closing: false,
  open: (conversationId, documentId, viewing = null) => set({ conversationId, documentId, viewing, closing: false }),
  view: (viewing) => set({ viewing }),
  close: () => {
    const gone = { documentId: null, conversationId: null, viewing: null, full: false, closing: false }
    if (get().documentId === null) return
    // The panel slides away before it goes (document.css); straight away
    // where motion is reduced.
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      set(gone)
      return
    }
    set({ closing: true })
    window.setTimeout(() => {
      if (get().closing) set(gone)
    }, PANEL_CLOSE_MS)
  },
  setFull: (full) => set({ full }),
}))
