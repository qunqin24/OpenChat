import type { ChatModel } from '@openchat/api-client'

/**
 * What can be attached. These mirror the server's limits (internal/api/files.go
 * and internal/agent/attachments.go) so a file is turned away here, with the
 * reason, instead of after it has been uploaded; the server enforces them again.
 */
export const MAX_FILES = 10
export const MAX_IMAGE_BYTES = 5 << 20
export const MAX_PDF_BYTES = 10 << 20
export const MAX_TEXT_BYTES = 256 << 10
/** A Word or Excel file goes as its words (internal/doctext); the file itself may be large. */
export const MAX_DOCUMENT_BYTES = 20 << 20
export const MAX_TOTAL_BYTES = 20 << 20

const IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/webp', 'image/gif']
export const PDF_TYPE = 'application/pdf'
/** The one type a text file is sent as, whatever it holds (llm.TextMediaType). */
export const TEXT_TYPE = 'text/plain'
export const DOCX_TYPE = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
export const XLSX_TYPE = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
const DOCUMENT_TYPES = [DOCX_TYPE, XLSX_TYPE]

/** Whether a file is a Word or Excel file, which the server reads as words. */
export function isDocument(mediaType: string): boolean {
  return DOCUMENT_TYPES.includes(mediaType)
}

/** Whether a sent file opens in the side panel (FilePanel): a text file, Word, Excel or PDF. */
export function opensInPanel(mediaType: string): boolean {
  return mediaType === TEXT_TYPE || isDocument(mediaType) || mediaType === PDF_TYPE
}

/** A Word or Excel file's type by its name. */
export function documentTypeOf(name: string): string {
  return name.toLowerCase().endsWith('.xlsx') ? XLSX_TYPE : DOCX_TYPE
}

/**
 * The extensions of the text files that can be attached: notes, data and
 * code. Known by name, because the browser names them inconsistently (a .ts
 * file is "video/mp2t", a .md often nothing); each is sent as text/plain.
 */
export const TEXT_EXTENSIONS = [
  'txt', 'text', 'md', 'markdown', 'mdx', 'rst', 'adoc', 'org', 'tex', 'log',
  'csv', 'tsv', 'json', 'jsonl', 'ndjson', 'yaml', 'yml', 'toml', 'ini', 'cfg', 'conf', 'properties', 'env', 'xml', 'svg',
  'html', 'htm', 'css', 'scss', 'sass', 'less',
  'js', 'mjs', 'cjs', 'jsx', 'ts', 'mts', 'cts', 'tsx', 'vue', 'svelte',
  'py', 'rb', 'php', 'go', 'rs', 'java', 'kt', 'kts', 'scala', 'swift', 'm', 'mm', 'c', 'h', 'cc', 'cpp', 'cxx', 'hpp', 'cs',
  'fs', 'dart', 'lua', 'pl', 'r', 'jl', 'ex', 'exs', 'erl', 'hs', 'clj', 'elm', 'zig', 'nim', 'sol',
  'sh', 'bash', 'zsh', 'fish', 'ps1', 'bat', 'cmd', 'sql', 'graphql', 'gql', 'proto', 'tf', 'hcl', 'gradle', 'cmake', 'mk',
  'srt', 'vtt', 'diff', 'patch',
]

/** Files known by their whole name rather than an extension. */
const TEXT_NAMES = ['dockerfile', 'makefile', 'license', 'readme', '.gitignore', '.editorconfig', '.env']

export type FileKind = 'image' | 'pdf' | 'text' | 'document'

/** The kinds of file a model takes, from its input modalities. */
export interface Accepts {
  image: boolean
  /** A model that reads PDFs, or any where the server sends a PDF's words (`pdfAsText`). */
  pdf: boolean
  /** Text, Word and Excel files go as words, which every model takes. */
  text: boolean
}

export function acceptsOf(model: (Pick<ChatModel, 'inputModalities'> & Partial<Pick<ChatModel, 'pdfAsText'>>) | undefined): Accepts {
  const modalities = model?.inputModalities ?? []
  return {
    image: modalities.includes('image'),
    pdf: modalities.includes('pdf') || model?.pdfAsText === true,
    text: model !== undefined,
  }
}

/** The `accept` attribute of the file picker: only what the model takes. */
export function acceptAttribute(accepts: Accepts): string {
  return [
    ...(accepts.image ? IMAGE_TYPES : []),
    ...(accepts.pdf ? [PDF_TYPE] : []),
    ...(accepts.text ? [TEXT_TYPE, ...TEXT_EXTENSIONS.map((extension) => `.${extension}`), ...DOCUMENT_TYPES, '.docx', '.xlsx'] : []),
  ].join(',')
}

/** Whether a file is one of the text files that can be attached, by its name. */
function isTextFile(file: File): boolean {
  const name = file.name.toLowerCase()
  const extension = name.includes('.') ? name.split('.').pop() : undefined
  if (TEXT_NAMES.includes(name)) return true
  if (extension && TEXT_EXTENSIONS.includes(extension)) return true
  // No extension it is known by: the browser's word, for text it is sure of.
  return !extension && file.type.startsWith('text/')
}

/** The media type of a file, from the browser or else its extension. */
export function mediaTypeOf(file: File): string {
  if (isTextFile(file)) return TEXT_TYPE
  const lower = file.name.toLowerCase()
  if (lower.endsWith('.docx')) return DOCX_TYPE
  if (lower.endsWith('.xlsx')) return XLSX_TYPE
  if (file.type) return file.type.toLowerCase()
  const extension = file.name.split('.').pop()?.toLowerCase()
  if (extension === 'pdf') return PDF_TYPE
  if (extension === 'docx') return DOCX_TYPE
  if (extension === 'xlsx') return XLSX_TYPE
  if (extension === 'png') return 'image/png'
  if (extension === 'jpg' || extension === 'jpeg') return 'image/jpeg'
  if (extension === 'webp') return 'image/webp'
  if (extension === 'gif') return 'image/gif'
  return ''
}

export function kindOf(mediaType: string): FileKind | null {
  if (IMAGE_TYPES.includes(mediaType)) return 'image'
  if (mediaType === TEXT_TYPE) return 'text'
  if (isDocument(mediaType)) return 'document'
  return mediaType === PDF_TYPE ? 'pdf' : null
}

/** Why a file cannot be attached; each is a `chat.file.reject.*` key. */
export type RejectReason = 'type' | 'model-image' | 'model-pdf' | 'size-image' | 'size-pdf' | 'size-text' | 'size-document' | 'empty'

const MAX_BYTES: Record<FileKind, number> = { image: MAX_IMAGE_BYTES, pdf: MAX_PDF_BYTES, text: MAX_TEXT_BYTES, document: MAX_DOCUMENT_BYTES }

export function rejectionOf(file: File, accepts: Accepts): RejectReason | null {
  const kind = kindOf(mediaTypeOf(file))
  if (!kind || ((kind === 'text' || kind === 'document') && !accepts.text)) return 'type'
  if (kind === 'image' && !accepts.image) return 'model-image'
  if (kind === 'pdf' && !accepts.pdf) return 'model-pdf'
  if (file.size === 0) return 'empty'
  if (file.size > MAX_BYTES[kind]) return `size-${kind}`
  return null
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`
  if (bytes < 1024 ** 3) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / 1024 ** 3).toFixed(bytes % 1024 ** 3 === 0 ? 0 : 1)} GB`
}
