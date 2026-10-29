import type { LibraryFile } from '@openchat/api-client'

import { MAX_IMAGE_BYTES } from '@/lib/attachments'

/**
 * The composer's other ways to attach files (its "+" menu), each ending in a File
 * that goes the way a picked one does (useAttachments): the model's accept
 * list, the size limits, the upload, the storage quota and, on sending,
 * content moderation all apply as they would to a file from the disk.
 */

/** Whether this browser can share a screen to capture: not phones. */
export function canCaptureScreen(): boolean {
  return typeof navigator !== 'undefined' && typeof navigator.mediaDevices?.getDisplayMedia === 'function'
}

/**
 * One still of a screen, window or tab the person picks in the browser's own
 * dialog; null when they close it. The stream is stopped as soon as the frame
 * is taken. A PNG, or a JPEG when the PNG is over an image's size limit (a
 * large screen in PNG can be).
 */
export async function captureScreen(name: string): Promise<File | null> {
  let stream: MediaStream
  try {
    stream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: false })
  } catch (error) {
    if (error instanceof DOMException && (error.name === 'NotAllowedError' || error.name === 'AbortError')) return null
    throw error
  }
  try {
    const video = document.createElement('video')
    video.muted = true
    video.playsInline = true
    video.srcObject = stream
    await video.play()
    // The first frame, painted: before it the video is 0 by 0.
    await new Promise<void>((resolve) => {
      if (video.videoWidth > 0) resolve()
      else video.addEventListener('resize', () => resolve(), { once: true })
    })
    await new Promise((resolve) => requestAnimationFrame(resolve))
    const canvas = document.createElement('canvas')
    canvas.width = video.videoWidth
    canvas.height = video.videoHeight
    canvas.getContext('2d')?.drawImage(video, 0, 0)
    const encode = (type: string, quality?: number) =>
      new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, type, quality))
    let blob = await encode('image/png')
    let ext = 'png'
    if (blob && blob.size > MAX_IMAGE_BYTES) {
      blob = await encode('image/jpeg', 0.88)
      ext = 'jpg'
    }
    if (!blob) throw new Error('the screen could not be captured')
    return new File([blob], `${name}.${ext}`, { type: blob.type })
  } finally {
    for (const track of stream.getTracks()) track.stop()
  }
}

/**
 * A file from the library, fetched by its signed link so it can be attached
 * again: a new upload, a copy of its own (a file belongs to the message it
 * was sent with, and goes when that message does).
 */
export async function libraryFileAsFile(file: LibraryFile): Promise<File> {
  const response = await fetch(file.url)
  if (!response.ok) throw new Error(`file ${response.status}`)
  const blob = await response.blob()
  return new File([blob], file.filename, { type: file.contentType || blob.type })
}
