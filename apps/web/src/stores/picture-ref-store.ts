import { create } from 'zustand'

import type { GeneratedPicture } from '@openchat/chat-core/image-tool'

// The pictures the person picked with 「拿来改」 for their next message: shown
// as chips on the composer, sent as `<image-ref id="img_…"/>` lines at the top
// of the message (chat-core/src/image-ref.ts), which tell the model which
// pictures to change.

interface PictureRefState {
  /** Whose composer they are for: picking in another conversation starts over. */
  conversationId: string | null
  pictures: GeneratedPicture[]
  add: (conversationId: string, picture: GeneratedPicture) => void
  remove: (handle: string) => void
  clear: () => void
}

/** At most this many pictures go with one message, as one image call may edit. */
export const MAX_PICTURE_REFS = 4

export const usePictureRefs = create<PictureRefState>()((set) => ({
  conversationId: null,
  pictures: [],
  add: (conversationId, picture) =>
    set((state) => {
      const pictures = state.conversationId === conversationId ? state.pictures : []
      if (pictures.some((p) => p.handle === picture.handle)) return { conversationId, pictures }
      return { conversationId, pictures: [...pictures, picture].slice(-MAX_PICTURE_REFS) }
    }),
  remove: (handle) => set((state) => ({ pictures: state.pictures.filter((p) => p.handle !== handle) })),
  clear: () => set({ conversationId: null, pictures: [] }),
}))
