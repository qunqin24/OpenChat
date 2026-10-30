import { isOpenChatError } from '@openchat/api-client'
import { cacheDirectory, createUploadTask, FileSystemUploadType } from 'expo-file-system/legacy'

import { serverUrl } from '@/config'
import { api, unwrap } from './api'

/** Why an upload failed, as the `chat.file.error.*` key that explains it. */
export type UploadError = 'type' | 'size' | 'quota' | 'failed'

export class UploadFailed extends Error {
  constructor(readonly reason: UploadError) {
    super(reason)
  }
}

/** A file on the device, picked or shared: where it is and what it is. */
export interface LocalFile {
  uri: string
  name: string
  /** The media type as the content type it is uploaded under (chat-core `mediaTypeOf`). */
  type: string
  size: number
}

/**
 * Where the server said to put the bytes: an S3 presigned URL as it is, the
 * local store's signed path on the server. Either way the signature is the
 * authorization - no session goes with it, so none reaches storage.
 */
export const onServer = (url: string) => (/^https?:\/\//i.test(url) ? url : `${serverUrl}${url.startsWith('/') ? '' : '/'}${url}`)

/** Where a file fetched from the server is kept on the device: the cache, under a name safe for a path. */
export const cachePath = (id: string, filename: string) => `${cacheDirectory}${id}-${filename.replace(/[^\w.-]+/g, '_')}`

/**
 * Uploads a file for attaching to a message and resolves with its id (the
 * web's lib/upload.ts): the API issues a signed URL, and the file goes there
 * straight from disk with a PUT carrying the content type it was signed for.
 */
export async function uploadFile(
  file: LocalFile,
  { signal, onProgress }: { signal: AbortSignal; onProgress: (fraction: number) => void },
): Promise<string> {
  let issued
  try {
    issued = unwrap(await api.POST('/files', { body: { filename: file.name, contentType: file.type, size: file.size }, signal }))
  } catch (error) {
    if (signal.aborted) throw error
    const code = isOpenChatError(error) ? error.code : ''
    if (code === 'unsupported_file_type') throw new UploadFailed('type')
    if (code === 'file_too_large') throw new UploadFailed('size')
    if (code === 'storage_quota_exceeded') throw new UploadFailed('quota')
    throw new UploadFailed('failed')
  }

  const task = createUploadTask(
    onServer(issued.uploadUrl),
    file.uri,
    { httpMethod: 'PUT', uploadType: FileSystemUploadType.BINARY_CONTENT, headers: { 'Content-Type': file.type } },
    ({ totalBytesSent, totalBytesExpectedToSend }) => {
      if (totalBytesExpectedToSend > 0) onProgress(totalBytesSent / totalBytesExpectedToSend)
    },
  )
  const cancel = () => void task.cancelAsync()
  signal.addEventListener('abort', cancel, { once: true })
  try {
    const result = await task.uploadAsync()
    if (signal.aborted) throw new Error('upload aborted')
    if (!result || result.status < 200 || result.status >= 300) throw new UploadFailed('failed')
  } catch (error) {
    if (error instanceof UploadFailed || signal.aborted) throw error
    throw new UploadFailed('failed')
  } finally {
    signal.removeEventListener('abort', cancel)
  }
  return issued.id
}
