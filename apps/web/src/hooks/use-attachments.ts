import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  MAX_FILES,
  MAX_TOTAL_BYTES,
  kindOf,
  type FileKind,
  mediaTypeOf,
  rejectionOf,
  type Accepts,
  type RejectReason,
} from '@/lib/attachments'
import { queryKeys } from '@/lib/query-keys'
import { UploadFailed, uploadFile, type UploadError } from '@/lib/upload'

/** A file on its way into the next message. */
export interface PendingAttachment {
  key: string
  name: string
  size: number
  kind: FileKind
  /** An object URL of the file itself, for the thumbnail of an image. */
  preview?: string
  status: 'uploading' | 'ready' | 'error'
  /** 0 to 1; meaningful while uploading. */
  progress: number
  /** The server's id for it, once uploaded. */
  id?: string
  error?: UploadError
}

/**
 * The files being attached to the message being written: picked or dropped,
 * uploaded at once (so sending is instant), and removable until sent.
 *
 * What the model takes is the caller's to say (`accepts`); a file it does not
 * take is turned away here with the reason, never uploaded.
 */
export function useAttachments(accepts: Accepts) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [items, setItems] = useState<PendingAttachment[]>([])
  // What is already queued, readable synchronously: a drop of several files is
  // judged against the room left by the ones before it.
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
    if (item.preview) URL.revokeObjectURL(item.preview)
  }, [])

  const add = useCallback(
    (files: File[]) => {
      const rejected = new Map<string, string>()
      const reject = (reason: RejectReason | 'count' | 'total', name: string) => {
        if (!rejected.has(reason)) {
          rejected.set(reason, t(`chat.file.reject.${reason}`, { name, max: reason === 'total' ? MAX_TOTAL_BYTES >> 20 : MAX_FILES }))
        }
      }

      let next = queued.current
      let total = next.reduce((sum, item) => sum + item.size, 0)
      const started: { item: PendingAttachment; file: File }[] = []
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
          key: crypto.randomUUID(),
          name: file.name,
          size: file.size,
          kind,
          preview: kind === 'image' ? URL.createObjectURL(file) : undefined,
          status: 'uploading',
          progress: 0,
        }
        next = [...next, item]
        total += file.size
        started.push({ item, file })
      }
      rejected.forEach((message, reason) => toast.error(message, { id: `attach-${reason}` }))
      if (started.length === 0) return
      commit(next)

      for (const { item, file } of started) {
        const controller = new AbortController()
        uploads.current.set(item.key, controller)
        uploadFile(file, {
          signal: controller.signal,
          onProgress: (progress) => patch(item.key, { progress }),
        }).then(
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
            if (reason === 'quota') toast.error(t('chat.file.storage-full'), { id: 'storage-full' })
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

  /** Lets go of everything, once the message is sent. Uploads in flight are cancelled. */
  const clear = useCallback(() => {
    queued.current.forEach(forget)
    commit([])
  }, [commit, forget])

  /**
   * Takes the files out of the composer as a message is sent, so they are not
   * shown twice - in the message and still in the box - while the server takes
   * it, which with a large PDF is a while. They are given back (`restore`) if
   * the send fails, and let go of (`release`) once it has gone through.
   */
  const take = useCallback(() => {
    const taken = queued.current
    commit([])
    return taken
  }, [commit])

  /** Puts files taken for a send that failed back in the composer, before any added since. */
  const restore = useCallback(
    (taken: PendingAttachment[]) => commit([...taken, ...queued.current.filter((item) => !taken.includes(item))]),
    [commit],
  )

  /** Lets go of files taken for a send that went through. */
  const release = useCallback((taken: PendingAttachment[]) => taken.forEach(forget), [forget])

  // Leaving the screen abandons what was still going up and frees the previews.
  useEffect(() => {
    const pending = uploads.current
    return () => {
      pending.forEach((controller) => controller.abort())
      queued.current.forEach((item) => item.preview && URL.revokeObjectURL(item.preview))
    }
  }, [])

  return {
    items,
    add,
    remove,
    clear,
    take,
    restore,
    release,
    uploading: items.some((item) => item.status === 'uploading'),
    failed: items.some((item) => item.status === 'error'),
    ids: items.flatMap((item) => (item.id ? [item.id] : [])),
  }
}
