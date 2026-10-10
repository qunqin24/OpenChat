import type { ChatModel } from '@openchat/api-client'

/**
 * What can be attached. These mirror the server's limits (internal/api/files.go
 * and internal/agent/attachments.go) so a file is turned away here, with the
 * reason, instead of after it has been uploaded; the server enforces them again.
 */
export const MAX_FILES = 10
export const MAX_IMAGE_BYTES = 5 << 20
export const MAX_PDF_BYTES = 10 << 20
export const MAX_TOTAL_BYTES = 20 << 20

const IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/webp', 'image/gif']
const PDF_TYPE = 'application/pdf'

export type FileKind = 'image' | 'pdf'

/** The kinds of file a model takes, from its input modalities. */
export interface Accepts {
  image: boolean
  pdf: boolean
}

export function acceptsOf(model: Pick<ChatModel, 'inputModalities'> | undefined): Accepts {
  const modalities = model?.inputModalities ?? []
  return { image: modalities.includes('image'), pdf: modalities.includes('pdf') }
}

/** The `accept` attribute of the file picker: only what the model takes. */
export function acceptAttribute(accepts: Accepts): string {
  return [...(accepts.image ? IMAGE_TYPES : []), ...(accepts.pdf ? [PDF_TYPE] : [])].join(',')
}

/** The media type of a file, from the browser or else its extension. */
export function mediaTypeOf(file: File): string {
  if (file.type) return file.type.toLowerCase()
  const extension = file.name.split('.').pop()?.toLowerCase()
  if (extension === 'pdf') return PDF_TYPE
  if (extension === 'png') return 'image/png'
  if (extension === 'jpg' || extension === 'jpeg') return 'image/jpeg'
  if (extension === 'webp') return 'image/webp'
  if (extension === 'gif') return 'image/gif'
  return ''
}

export function kindOf(mediaType: string): FileKind | null {
  if (IMAGE_TYPES.includes(mediaType)) return 'image'
  return mediaType === PDF_TYPE ? 'pdf' : null
}

/** Why a file cannot be attached; each is a `next.file.reject.*` key. */
export type RejectReason = 'type' | 'model-image' | 'model-pdf' | 'size-image' | 'size-pdf' | 'empty'

export function rejectionOf(file: File, accepts: Accepts): RejectReason | null {
  const kind = kindOf(mediaTypeOf(file))
  if (!kind) return 'type'
  if (!accepts[kind]) return kind === 'image' ? 'model-image' : 'model-pdf'
  if (file.size === 0) return 'empty'
  if (file.size > (kind === 'image' ? MAX_IMAGE_BYTES : MAX_PDF_BYTES)) {
    return kind === 'image' ? 'size-image' : 'size-pdf'
  }
  return null
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}
