import { create } from 'zustand'

import { chatStateCreator, initialChatState, type ChatState } from '@openchat/chat-core/chat-store'
import { useDocumentActivity } from './document-activity-store'
import { useDocumentPanel } from './document-panel-store'
import { useDocumentQuote } from './document-quote-store'
import { useFilePanel } from './file-panel-store'
import { useImageViewer } from './image-viewer-store'
import { useSubagentPanel } from './subagent-panel-store'

/** The chat screen's store (chat-core chat-store, which the mobile app shares). */
export const useChatStore = create<ChatState>()(chatStateCreator)

/**
 * Back to a blank page: the state of one person's chat screen must not be seen
 * by the next. Used when the account changes, and when the session ends.
 */
export function resetChatStore() {
  useChatStore.setState(initialChatState)
  // Every other store that holds what one person's chat shows: an image of
  // theirs on a signed URL, a passage of their document, which panel is open.
  useImageViewer.setState({ images: [], index: 0 })
  useDocumentPanel.setState({ documentId: null, conversationId: null, viewing: null, full: false, closing: false })
  useDocumentQuote.setState({ quotes: [] })
  useDocumentActivity.setState({ edits: {} })
  useSubagentPanel.getState().close()
  useFilePanel.getState().close()
}
