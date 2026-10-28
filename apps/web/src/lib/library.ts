import type { InfiniteData } from '@tanstack/react-query'
import type { LibraryFilePage, LibraryFileType } from '@openchat/api-client'

import type { TranscriptPages } from './transcript-pages'

/**
 * The library (`/library`): every file the person sent, to look through, save
 * or delete. These are the pure moves on the caches a delete touches; the hooks
 * are in `hooks/use-library.ts`.
 */

export type LibraryFilter = 'all' | LibraryFileType

export const libraryFilters: LibraryFilter[] = ['all', 'image', 'pdf', 'document', 'text']

export type LibraryPages = InfiniteData<LibraryFilePage, string | undefined>

/** The library's pages without one file. */
export function withoutFile(held: LibraryPages | undefined, fileId: string): LibraryPages | undefined {
  if (!held) return held
  return { ...held, pages: held.pages.map((page) => ({ ...page, items: page.items.filter((item) => item.id !== fileId) })) }
}

/**
 * A transcript with a deleted file moved from the message it came with to that
 * message's `removedAttachments`, as the server now reports it. The same object
 * when no message held it.
 */
export function markRemoved(held: TranscriptPages | undefined, fileId: string): TranscriptPages | undefined {
  if (!held) return held
  let changed = false
  const pages = held.pages.map((page) => {
    const items = page.items.map((message) => {
      const file = message.attachments?.find((item) => item.id === fileId)
      if (!file) return message
      changed = true
      return {
        ...message,
        attachments: message.attachments?.filter((item) => item.id !== fileId),
        removedAttachments: [
          ...(message.removedAttachments ?? []),
          { id: file.id, filename: file.filename, contentType: file.contentType },
        ],
      }
    })
    return { ...page, items }
  })
  return changed ? { ...held, pages } : held
}
