import { create } from 'zustand'

import { useDocumentPanel } from './document-panel-store'
import { useSubagentPanel } from './subagent-panel-store'

/** A text file opened beside the chat. */
export interface OpenFile {
  id: string
  filename: string
  contentType: string
  size: number
  url: string
}

/** Where a file opened from the library is shown, in place of a conversation. */
export const LIBRARY = 'library'

interface FilePanelState {
  /**
   * Where the file was opened - its conversation, or LIBRARY - and the file;
   * null when it is closed. A page shows the panel only for its own.
   */
  conversationId: string | null
  file: OpenFile | null
  open: (conversationId: string, file: OpenFile) => void
  close: () => void
}

/**
 * A text file someone sent, read in the side panel (FilePanel) rather than a
 * new tab, as ChatGPT opens one. It takes the writing block's and the
 * sub-agents' place beside the chat: opening one puts the others away.
 */
export const useFilePanel = create<FilePanelState>()((set) => ({
  conversationId: null,
  file: null,
  open: (conversationId, file) => {
    useDocumentPanel.getState().close()
    useSubagentPanel.getState().close()
    set({ conversationId, file })
  },
  close: () => set({ conversationId: null, file: null }),
}))

// A writing block or the sub-agents opened put the file away.
useDocumentPanel.subscribe((state, previous) => {
  if (state.documentId !== null && previous.documentId === null) useFilePanel.getState().close()
})
useSubagentPanel.subscribe((state, previous) => {
  if (state.conversationId !== null && previous.conversationId === null) useFilePanel.getState().close()
})
