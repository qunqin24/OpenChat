import type { ToolCallPart, ToolResultPart } from '@openchat/api-client'

// The image tool in a reply (docs/design/image-generation-plan.md): a call
// that makes pictures is drawn in the reply itself - a placeholder while the
// pictures are made, the pictures once they are - and one that made none is a
// failed step like any other. The pictures are the person's files, shown from
// `GET /files/{id}/view`.

export const IMAGE_TOOL = 'image_generate'

/** A shape as width:height, or auto (llm.Aspect). */
export type ImageAspect = 'auto' | '1:1' | '3:2' | '2:3' | '4:3' | '3:4' | '16:9' | '9:16' | '21:9'

/** What the model asked for, as far as its arguments have arrived. */
export interface ImageRequest {
  prompt: string
  aspect: ImageAspect
  /** An exact size asked for, when one was. */
  size?: { width: number; height: number }
  count: number
}

/** One picture a call made (api/image_tool.go ImageToolPicture). */
export interface GeneratedPicture {
  id: string
  handle: string
  mediaType: string
  width: number
  height: number
}

const ASPECTS: ImageAspect[] = ['auto', '1:1', '3:2', '2:3', '4:3', '3:4', '16:9', '9:16', '21:9']
/** The words the tool also takes for three of them. */
const WORDS: Record<string, ImageAspect> = { square: '1:1', landscape: '3:2', portrait: '2:3' }

function sizeOf(value: unknown): { width: number; height: number } | undefined {
  if (typeof value !== 'string') return undefined
  const match = /^\s*(\d{3,4})\s*[x×]\s*(\d{3,4})\s*$/i.exec(value)
  if (!match) return undefined
  const width = Number(match[1])
  const height = Number(match[2])
  return width >= 256 && height >= 256 && width <= 4096 && height <= 4096 ? { width, height } : undefined
}

/** The call's arguments; null while there is no prompt to show yet. */
export function imageRequestOf(call: Pick<ToolCallPart, 'arguments'>): ImageRequest | null {
  const args = call.arguments as Record<string, unknown> | undefined
  if (!args || typeof args !== 'object') return null
  const prompt = typeof args.prompt === 'string' ? args.prompt.trim() : ''
  const raw = typeof args.aspect === 'string' ? args.aspect.trim().toLowerCase() : ''
  const aspect = WORDS[raw] ?? (ASPECTS.includes(raw as ImageAspect) ? (raw as ImageAspect) : 'auto')
  const count = typeof args.count === 'number' && Number.isFinite(args.count) ? Math.min(Math.max(Math.round(args.count), 1), 10) : 1
  const size = sizeOf(args.size)
  return size ? { prompt, aspect, size, count } : { prompt, aspect, count }
}

function isPicture(value: unknown): value is GeneratedPicture {
  if (typeof value !== 'object' || value === null) return false
  const p = value as Record<string, unknown>
  return typeof p.id === 'string' && typeof p.handle === 'string' && typeof p.width === 'number' && typeof p.height === 'number'
}

/** The pictures a finished call made: none for a call that made none. */
export function picturesOf(result: Pick<ToolResultPart, 'status' | 'details'> | undefined): GeneratedPicture[] {
  if (!result || result.status !== 'succeeded') return []
  const images = (result.details as { images?: unknown } | undefined)?.images
  return Array.isArray(images) ? images.filter(isPicture) : []
}

/**
 * The size a placeholder is drawn at and labelled with: the exact size asked
 * for, else the shape at the usual size (the longer side 1536, a square 1024).
 * Null for auto: the size is the model's to choose.
 */
export function placeholderSize(request: Pick<ImageRequest, 'aspect' | 'size'>): { width: number; height: number } | null {
  if (request.size) return request.size
  if (request.aspect === 'auto') return null
  const [w, h] = request.aspect.split(':').map(Number) as [number, number]
  if (w === h) return { width: 1024, height: 1024 }
  return w > h ? { width: 1536, height: Math.round((1536 * h) / w) } : { width: Math.round((1536 * w) / h), height: 1536 }
}

/** Where a picture is drawn from: a redirect to a short-lived address, so it never goes stale. */
export function pictureSrc(apiBase: string, id: string): string {
  return `${apiBase}/files/${encodeURIComponent(id)}/view`
}
