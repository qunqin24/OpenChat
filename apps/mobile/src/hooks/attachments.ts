import { useQueryClient } from '@tanstack/react-query'
import * as Crypto from 'expo-crypto'
import { getInfoAsync } from 'expo-file-system/legacy'
import { ImageManipulator, SaveFormat } from 'expo-image-manipulator'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  kindOf,
  MAX_FILES,
  MAX_IMAGE_BYTES,
  MAX_TOTAL_BYTES,
  mediaTypeOf,
  rejectionOf,
  type Accepts,
  type FileKind,
  type RejectReason,
} from '@openchat/chat-core/attachments'
import { queryKeys } from '@openchat/chat-core/query-keys'

import { toast } from '@/lib/toast'
import { UploadFailed, uploadFile, type LocalFile, type UploadError } from '@/lib/upload'

/** A file on its way into the next message. */
export interface PendingAttachment {
  key: string
  name: string
  size: number
  kind: FileKind
  /** The file on the device, for an image's thumbnail. */
  preview?: string
  status: 'preparing' | 'uploading' | 'ready' | 'error'
  /** 0 to 1; meaningful while uploading. */
  progress: number
  /** The server's id for it, once uploaded. */
  id?: string
  error?: UploadError
}

/** The longest side a photo is sent at: plenty for a model to read, and it keeps a phone's photo under the limit. */
const MAX_IMAGE_SIDE = 2560
const IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/webp', 'image/gif']

/**
 * A photo as it is sent: one the model cannot take as it is - a phone's HEIC,
 * or larger than an image may be - is redrawn as a JPEG, its longest side at
 * most MAX_IMAGE_SIDE. Anything else goes as it is.
 */
async function sendable(file: LocalFile & { width?: number; height?: number }): Promise<LocalFile> {
  if (!file.type.startsWith('image/') || file.type === 'image/gif') return file
  const large = file.size > MAX_IMAGE_BYTES || Math.max(file.width ?? 0, file.height ?? 0) > MAX_IMAGE_SIDE
  if (IMAGE_TYPES.includes(file.type) && !large) return file
  const context = ImageManipulator.manipulate(file.uri)
  if (large) {
    context.resize((file.width ?? 0) >= (file.height ?? 0) ? { width: MAX_IMAGE_SIDE } : { height: MAX_IMAGE_SIDE })
  }
  const image = await context.renderAsync()
  const saved = await image.saveAsync({ compress: 0.85, format: SaveFormat.JPEG })
  const info = await getInfoAsync(saved.uri)
  const name = file.name.replace(/\.[^.]+$/, '') + '.jpg'
  return { uri: saved.uri, name, type: 'image/jpeg', size: info.exists ? info.size : file.size }
}

/**
 * The files being attached to the message being written (the web's
 * useAttachments): picked or shared, uploaded at once so sending is instant,
 * removable until sent. What the model takes is the caller's to say
 * (`accepts`); a file it does not take is turned away with the reason, never
 * uploaded.
 */
export function useAttachments(accepts: Accepts) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [items, setItems] = useState<PendingAttachment[]>([])
  // What is already queued, readable synchronously: several files at once are
  // judged against the room left by the ones before them.
  const queued = useRef<PendingAttachment[]>([])
  const uploads = useRef(new Map<string, AbortController>())

  const commit = useCallback((next: PendingAttachment[]) => {
    queued.current = next
    setItems(next)
  }, [])
  const patch = useCallback(
    (key: string, change: Partial<PendingAttachment>) =>
      commit(queued.current.map((item) => (item.key === key ? { ...item, ...change } : item))),
    [commit],
  )
  const forget = useCallback((item: PendingAttachment) => {
    uploads.current.get(item.key)?.abort()
    uploads.current.delete(item.key)
  }, [])

  const add = useCallback(
    async (picked: (LocalFile & { width?: number; height?: number })[]) => {
      const rejected = new Map<string, string>()
      const reject = (reason: RejectReason | 'count' | 'total', name: string) => {
        if (!rejected.has(reason)) {
          rejected.set(reason, t(`chat.file.reject.${reason}`, { name, max: reason === 'total' ? MAX_TOTAL_BYTES >> 20 : MAX_FILES }))
        }
      }
      // Photos are made sendable first: what is judged is what would go up.
      const files: LocalFile[] = []
      for (const raw of picked) {
        const typed = { ...raw, type: mediaTypeOf(raw) || raw.type }
        try {
          files.push(await sendable(typed))
        } catch {
          files.push(typed)
        }
      }

      let next = queued.current
      let total = next.reduce((sum, item) => sum + item.size, 0)
      const started: { item: PendingAttachment; file: LocalFile }[] = []
      for (const file of files) {
        const reason = rejectionOf(file, accepts)
        if (reason) {
          reject(reason, file.name)
          continue
        }
        if (next.length >= MAX_FILES) {
          reject('count', file.name)
          continue
        }
        if (total + file.size > MAX_TOTAL_BYTES) {
          reject('total', file.name)
          continue
        }
        const kind = kindOf(mediaTypeOf(file))!
        const item: PendingAttachment = {
          key: Crypto.randomUUID(),
          name: file.name,
          size: file.size,
          kind,
          preview: kind === 'image' ? file.uri : undefined,
          status: 'uploading',
          progress: 0,
        }
        next = [...next, item]
        total += file.size
        started.push({ item, file })
      }
      rejected.forEach((message) => toast.error(message))
      if (started.length === 0) return
      commit(next)

      for (const { item, file } of started) {
        const controller = new AbortController()
        uploads.current.set(item.key, controller)
        uploadFile(file, { signal: controller.signal, onProgress: (progress) => patch(item.key, { progress }) }).then(
          (id) => {
            uploads.current.delete(item.key)
            patch(item.key, { status: 'ready', progress: 1, id })
            void queryClient.invalidateQueries({ queryKey: queryKeys.storage })
          },
          (error: unknown) => {
            uploads.current.delete(item.key)
            if (controller.signal.aborted) return // removed while uploading
            const reason = error instanceof UploadFailed ? error.reason : 'failed'
            patch(item.key, { status: 'error', error: reason })
            // Full is not this file's fault: said once, with what to do.
            if (reason === 'quota') toast.error(t('chat.file.storage-full'))
          },
        )
      }
    },
    [accepts, commit, patch, queryClient, t],
  )

  const remove = useCallback(
    (key: string) => {
      const item = queued.current.find((candidate) => candidate.key === key)
      if (!item) return
      forget(item)
      commit(queued.current.filter((candidate) => candidate.key !== key))
    },
    [commit, forget],
  )

  /** Takes the files out of the composer as a message is sent; `restore` or `release` follows. */
  const take = useCallback(() => {
    const taken = queued.current
    commit([])
    return taken
  }, [commit])
  /** Puts files taken for a send that failed back, before any added since. */
  const restore = useCallback(
    (taken: PendingAttachment[]) => commit([...taken, ...queued.current.filter((item) => !taken.includes(item))]),
    [commit],
  )
  /** Lets go of files taken for a send that went through. */
  const release = useCallback((taken: PendingAttachment[]) => taken.forEach(forget), [forget])

  // Leaving the screen abandons what was still going up.
  useEffect(() => {
    const pending = uploads.current
    return () => pending.forEach((controller) => controller.abort())
  }, [])

  return {
    items,
    add,
    remove,
    take,
    restore,
    release,
    uploading: items.some((item) => item.status === 'uploading' || item.status === 'preparing'),
    failed: items.some((item) => item.status === 'error'),
    ids: items.flatMap((item) => (item.id ? [item.id] : [])),
  }
}
