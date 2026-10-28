import { create } from 'zustand'

/** One picture the viewer can show. */
export interface ViewerImage {
  src: string
  /** What it is: a file's name, a caption, the alt text. */
  title: string
  /** A page the picture came from, offered as "open source". */
  link?: string
  /** Saves the file, when there is a file to save (an attachment of the person's). */
  download?: () => void
}

interface ImageViewerState {
  images: ViewerImage[]
  index: number
  open: (images: ViewerImage[], index: number) => void
  close: () => void
  go: (step: 1 | -1) => void
}

/**
 * The picture shown full size over the page (ImageViewer, drawn once by the
 * chat shell), and the others of its group to step through: the images of one
 * message, of one answer, of the library list.
 */
export const useImageViewer = create<ImageViewerState>()((set) => ({
  images: [],
  index: 0,
  open: (images, index) => set({ images, index: Math.min(Math.max(index, 0), images.length - 1) }),
  close: () => set({ images: [], index: 0 }),
  go: (step) =>
    set((state) => {
      const n = state.images.length
      return n > 1 ? { index: (state.index + step + n) % n } : state
    }),
}))
