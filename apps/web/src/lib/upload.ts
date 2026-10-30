import { isOpenChatError } from '@openchat/api-client'

import { api, unwrap } from '@/lib/api'
import { mediaTypeOf } from '@openchat/chat-core/attachments'

/** Why an upload failed, as the `chat.file.error.*` key that explains it. */
export type UploadError = 'type' | 'size' | 'quota' | 'failed'

export class UploadFailed extends Error {
  constructor(readonly reason: UploadError) {
    super(reason)
  }
}

/**
 * Uploads a file for attaching to a message and resolves with its id.
 *
 * The bytes do not go through the API: it issues a presigned URL and the file
 * is PUT straight to blob storage. `fetch` cannot report upload progress, hence
 * the XHR. The PUT must carry the same Content-Type the URL was signed for.
 */
export async function uploadFile(
  file: File,
  { signal, onProgress }: { signal: AbortSignal; onProgress: (fraction: number) => void },
): Promise<string> {
  const contentType = mediaTypeOf(file)
  let issued
  try {
    issued = unwrap(
      await api.POST('/files', {
        body: { filename: file.name, contentType, size: file.size },
        signal,
      }),
    )
  } catch (error) {
    if (signal.aborted) throw error
    const code = isOpenChatError(error) ? error.code : ''
    if (code === 'unsupported_file_type') throw new UploadFailed('type')
    if (code === 'file_too_large') throw new UploadFailed('size')
    if (code === 'storage_quota_exceeded') throw new UploadFailed('quota')
    throw new UploadFailed('failed')
  }

  await new Promise<void>((resolve, reject) => {
    const request = new XMLHttpRequest()
    request.open('PUT', issued.uploadUrl)
    request.setRequestHeader('Content-Type', contentType)
    request.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress(event.loaded / event.total)
    }
    request.onload = () =>
      request.status >= 200 && request.status < 300 ? resolve() : reject(new UploadFailed('failed'))
    request.onerror = () => reject(new UploadFailed('failed'))
    request.onabort = () => reject(new DOMException('Upload aborted', 'AbortError'))
    signal.addEventListener('abort', () => request.abort(), { once: true })
    if (signal.aborted) return reject(new DOMException('Upload aborted', 'AbortError'))
    request.send(file)
  })
  return issued.id
}
