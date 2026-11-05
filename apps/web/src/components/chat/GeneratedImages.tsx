import type { ToolCallPart, ToolResultPart } from '@openchat/api-client'
import { Download, PenLine } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { API_BASE_URL } from '@/lib/api'
import { downloadFile } from '@/hooks/use-library'
import { cn } from '@/lib/utils'
import { useImageViewer, type ViewerImage } from '@/stores/image-viewer-store'
import { usePictureRefs } from '@/stores/picture-ref-store'
import { imageRequestOf, pictureSrc, picturesOf, placeholderSize, type GeneratedPicture, type ImageRequest } from '@openchat/chat-core/image-tool'
import DevelopingPicture from './DevelopingPicture'

// The pictures an image call made, in the reply itself (design A: frameless,
// at their own shape, actions on hover), or the placeholders while they are
// being made (「显影 · 对焦」, DevelopingPicture). A call that made none is
// a failed step of the activity, not this (MessageItem).

interface Props {
  call: ToolCallPart
  result?: ToolResultPart
  /** The call is being made now, as opposed to waiting or long finished. */
  working: boolean
  conversationId?: string
}

export default function GeneratedImages({ call, result, working, conversationId }: Props) {
  const request = imageRequestOf(call)
  const pictures = picturesOf(result)
  // Pictures that arrive while the reader watches come into focus as the
  // placeholder did; ones drawn from history are simply there.
  // Whether it was first drawn before its result: then the reader is watching.
  const [watched] = useState(() => !result)

  if (pictures.length > 0) {
    return <Pictures pictures={pictures} prompt={request?.prompt ?? ''} fresh={watched} conversationId={conversationId} />
  }
  if (result || !request) return null
  return <Developing request={request} working={working} callId={call.id} />
}

function Developing({ request, working, callId }: { request: ImageRequest; working: boolean; callId: string }) {
  const { t } = useTranslation()
  const { prompt, count } = request
  // Auto has no size of its own yet: drawn square, labelled with none.
  const size = placeholderSize(request)
  const shape = size ?? { width: 1, height: 1 }
  const seconds = useSeconds(working)
  // Each call starts from a palette of its own, so two side by side differ.
  const seed = [...callId].reduce((sum, c) => sum + c.charCodeAt(0), 0)
  return (
    <figure className="n-pictures-developing" aria-busy="true" aria-label={t('chat.picture.making')}>
      <div className={cn('n-pictures', count > 1 && 'is-grid')}>
        {Array.from({ length: count }, (_, i) => (
          <div key={i} className="n-picture is-developing" style={{ aspectRatio: `${shape.width} / ${shape.height}` }} data-motion="essential">
            <DevelopingPicture seed={seed + i * 3} />
          </div>
        ))}
      </div>
      <figcaption className="n-picture-meta">
        <span className="n-picture-status">{t('chat.picture.making')}</span>
        <span className="n-picture-size">
          {size ? `${size.width} × ${size.height}` : ''}
          {size && working ? ' · ' : ''}
          {working ? t('chat.picture.seconds', { count: seconds }) : ''}
        </span>
        {prompt && <span className="n-picture-prompt">{prompt}</span>}
      </figcaption>
    </figure>
  )
}

/** Seconds since the call was first seen being made. */
function useSeconds(running: boolean) {
  const [started] = useState(() => Date.now())
  const [now, setNow] = useState(started)
  useEffect(() => {
    if (!running) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [running])
  return Math.max(0, Math.floor((now - started) / 1000))
}

function Pictures({ pictures, prompt, fresh, conversationId }: {
  pictures: GeneratedPicture[]
  prompt: string
  fresh: boolean
  conversationId?: string
}) {
  const { t } = useTranslation()
  const open = useImageViewer((s) => s.open)
  const pick = usePictureRefs((s) => s.add)
  const save = (picture: GeneratedPicture) => void downloadFile(picture.id).catch(() => toast.error(t('library.download-failed')))
  const gallery: ViewerImage[] = pictures.map((picture) => ({
    src: pictureSrc(API_BASE_URL, picture.id),
    title: prompt || picture.handle,
    download: () => save(picture),
  }))
  return (
    <div className={cn('n-pictures', pictures.length > 1 && 'is-grid')}>
      {pictures.map((picture, i) => (
        <div key={picture.id} className={cn('n-picture', fresh && 'is-fresh')} style={{ aspectRatio: `${picture.width} / ${picture.height}` }}>
          <button type="button" className="n-picture-open" aria-label={t('chat.picture.open')} onClick={() => open(gallery, i)}>
            <img src={gallery[i]!.src} alt={prompt} loading="lazy" width={picture.width} height={picture.height} />
          </button>
          <div className="n-picture-actions">
            {conversationId && (
              <button
                type="button"
                className="n-picture-action"
                aria-label={t('chat.picture.edit')}
                title={t('chat.picture.edit')}
                onClick={() => {
                  pick(conversationId, picture)
                  document.querySelector<HTMLTextAreaElement>('.n-composer textarea')?.focus()
                }}
              >
                <PenLine aria-hidden="true" />
              </button>
            )}
            <button type="button" className="n-picture-action" aria-label={t('chat.picture.download')} title={t('chat.picture.download')} onClick={() => save(picture)}>
              <Download aria-hidden="true" />
            </button>
          </div>
        </div>
      ))}
    </div>
  )
}
