/**
 * How a file is drawn, from its name and type: the icon and its colour in the
 * composer, a sent message, the library and the file panel (FileIcon). Text
 * files all arrive as text/plain, so it is the extension that tells notes
 * from data, settings, a web page and code.
 */
export type FileLook = 'note' | 'doc' | 'data' | 'config' | 'web' | 'code' | 'pdf' | 'image' | 'file'

const NOTE = new Set(['txt', 'text', 'md', 'markdown', 'mdx', 'rst', 'adoc', 'org', 'tex', 'log', 'srt', 'vtt'])
const DATA = new Set(['csv', 'tsv'])
const CONFIG = new Set(['json', 'jsonl', 'ndjson', 'yaml', 'yml', 'toml', 'ini', 'cfg', 'conf', 'properties', 'env', 'xml'])
const WEB = new Set(['html', 'htm', 'svg'])
const NOTE_NAMES = new Set(['license', 'readme'])

export function fileLook(filename: string, contentType: string): FileLook {
  if (contentType.startsWith('image/')) return 'image'
  if (contentType === 'application/pdf') return 'pdf'
  // Word is a document; Excel reads as data, its words being CSV.
  if (contentType === 'application/vnd.openxmlformats-officedocument.wordprocessingml.document') return 'doc'
  if (contentType === 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet') return 'data'
  if (contentType !== 'text/plain') return 'file'
  const name = filename.toLowerCase()
  const dot = name.lastIndexOf('.')
  const ext = dot > 0 ? name.slice(dot + 1) : ''
  if (NOTE.has(ext) || (dot <= 0 && NOTE_NAMES.has(name))) return 'note'
  if (DATA.has(ext)) return 'data'
  if (CONFIG.has(ext) || name === '.env' || name === '.editorconfig' || name === '.gitignore') return 'config'
  if (WEB.has(ext)) return 'web'
  return 'code'
}

/** Whether the file is shown as text in a monospace face: data, settings and code. */
export function looksLikeCode(look: FileLook): boolean {
  return look === 'data' || look === 'config' || look === 'web' || look === 'code'
}

/**
 * What a file's badge says (FileBadge): its extension, at most four letters
 * (DOCX, XLSX, PDF, MD); for a name with none, what kind it is.
 */
export function badgeText(filename: string, contentType: string): string {
  if (contentType === 'application/pdf') return 'PDF'
  const name = filename.toLowerCase()
  const dot = name.lastIndexOf('.')
  const ext = dot > 0 ? name.slice(dot + 1) : ''
  if (ext === 'markdown') return 'MD'
  if (ext && /^[a-z0-9]+$/.test(ext)) return ext.slice(0, 4).toUpperCase()
  return contentType.startsWith('image/') ? 'IMG' : contentType === 'text/plain' ? 'TXT' : 'FILE'
}

/** The kind a file is called by under its name (`chat.file.kind.*`). */
export type FileKindName = 'word' | 'excel' | 'pdf' | 'note' | 'data' | 'config' | 'web' | 'code' | 'image' | 'file'

export function fileKind(filename: string, contentType: string): FileKindName {
  if (contentType === 'application/vnd.openxmlformats-officedocument.wordprocessingml.document') return 'word'
  if (contentType === 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet') return 'excel'
  const look = fileLook(filename, contentType)
  return look === 'doc' ? 'word' : look
}

/** Whether a text file is Markdown, by its name: drawn as Markdown in the file panel. */
export function isMarkdownFile(filename: string): boolean {
  return /\.(md|markdown|mdx)$/i.test(filename)
}
