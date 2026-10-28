import { createFileRoute } from '@tanstack/react-router'

import LibraryPage from '@/components/chat/LibraryPage'

/** The library: the files sent in every conversation, beside the chat in the same shell. */
export const Route = createFileRoute('/_authed/library')({
  component: LibraryPage,
})
