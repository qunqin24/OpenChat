import type { ToolCallPart, ToolResultPart } from '@openchat/api-client'
import { Download, PenLine } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { API_BASE_URL } from '@/lib/api'
import { downloadFile } from '@/hooks/use-library'
import { cn } from '@/lib/utils'
import { useImageViewer, type ViewerImage } from '@/stores/image-viewer-store'
import { usePictureRefs } from '@/stores/picture-ref-store'
import { imageRequestOf, pictureSrc, picturesOf, placeholderSize, type GeneratedPicture } from '@openchat/chat-core/image-tool'
import DevelopingPicture from './DevelopingPicture'

// The pictures an image call made, in the reply itself (design A: frameless,
// at their own shape, actions on hover), and before them the placeholders
// (「显影 · 对焦」, DevelopingPicture). One card goes from one to the other:
// the colour field keeps moving under where the picture will be, the picture
// comes into focus over it once it has loaded, and only then is the field
// taken away and the 「正在生成图片」 line folded up. A call that made none
// is a failed step of the activity, not this (MessageItem).

interface Props {
  call: ToolCallPart
  result?: ToolResultPart
  conversationId?: string
}

export default function GeneratedImages({ call, result, conversationId }: Props) {
  const { t } = useTranslation()
  const request = imageRequestOf(call)
  const pictures = picturesOf(result)
  // Whether it was first drawn before its result: then the reader watched
  // it being made, and the pictures arrive into the placeholders. Drawn from
  // history, the pictures are simply there.
  const [watched] = useState(() => !result)
  // How many pictures have come into focus; the line under them goes once
  // every one has.
  const [settled, setSettled] = useState(0)
  const [captionGone, setCaptionGone] = useState(false)
  const countSettled = useCallback(() => setSettled((n) => n + 1), [])

  const leaving = pictures.length > 0 && settled >= pictures.length
  // Gone from the tree once it has folded up (pictures.css: 0.5 s).
  useEffect(() => {
    if (!leaving) return
    const timer = window.setTimeout(() => setCaptionGone(true), 600)
    return () => window.clearTimeout(timer)
  }, [leaving])

  if (result && pictures.length === 0) return null
  if (!request && pictures.length === 0) return null

  // Each call starts from a palette of its own, so two side by side differ.
  const seed = [...call.id].reduce((sum, c) => sum + c.charCodeAt(0), 0)
  // The shape asked for, so the picture takes its place without a jump; auto
  // has none yet and is drawn square. The size is not shown.
  const shape = (request && placeholderSize(request)) ?? { width: 1, height: 1 }
  const count = pictures.length > 0 ? pictures.length : (request?.count ?? 1)
  const developing = pictures.length === 0
  const prompt = request?.prompt ?? ''

  return (
    <figure className="n-pictures-figure" aria-busy={developing} aria-label={developing ? t('chat.picture.making') : undefined}>
      <div className={cn('n-pictures', count > 1 && 'is-grid')}>
        {Array.from({ length: count }, (_, i) => (
          <Tile
            // By place, not by picture: the card that was the placeholder
            // becomes the picture, and its colour field runs on unbroken.
            key={i}
            picture={pictures[i]}
            pictures={pictures}
            index={i}
            shape={shape}
            seed={seed + i * 3}
            fresh={watched}
            prompt={prompt}
            conversationId={conversationId}
            onSettled={countSettled}
          />
        ))}
      </div>
      {watched && !captionGone && (
        <figcaption
          className={cn('n-picture-meta', leaving && 'is-leaving')}
        >
          <span className="n-picture-status">{t('chat.picture.making')}</span>
        </figcaption>
      )}
    </figure>
  )
}

function Tile({ picture, pictures, index, shape, seed, fresh, prompt, conversationId, onSettled }: {
  picture?: GeneratedPicture
  pictures: GeneratedPicture[]
  index: number
  shape: { width: number; height: number }
  seed: number
  fresh: boolean
  prompt: string
  conversationId?: string
  onSettled: () => void
}) {
  const { t } = useTranslation()
  const open = useImageViewer((s) => s.open)
  const pick = usePictureRefs((s) => s.add)
  // The picture has loaded (so it comes into focus), and has finished coming
  // in (so the field under it can go).
  const [loaded, setLoaded] = useState(false)
  const [settled, setSettled] = useState(!fresh)

  const save = (p: GeneratedPicture) => void downloadFile(p.id).catch(() => toast.error(t('library.download-failed')))
  const ratio = picture ? `${picture.width} / ${picture.height}` : `${shape.width} / ${shape.height}`
  // In once its transition has run (pictures.css: 1.1 s, 0.25 s with
  // reduced motion) - timed rather than told by transitionend, which does
  // not always come.
  useEffect(() => {
    if (!loaded || settled) return
    const still = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    const timer = window.setTimeout(() => {
      setSettled(true)
      onSettled()
    }, still ? 300 : 1150)
    return () => window.clearTimeout(timer)
  }, [loaded, settled, onSettled])
  const gallery = (): ViewerImage[] =>
    pictures.map((p) => ({
      src: pictureSrc(API_BASE_URL, p.id),
      // Not the prompt: that is the model's words for the image model.
      title: t('chat.picture.viewer-title'),
      download: () => save(p),
    }))

  return (
    <div
      className={cn('n-picture', !settled && 'is-developing', fresh && 'is-arriving', loaded && 'is-loaded')}
      style={{ aspectRatio: ratio }}
      data-motion={settled ? undefined : 'essential'}
    >
      {!settled && <DevelopingPicture seed={seed} />}
      {picture && (
        <>
          <button type="button" className="n-picture-open" aria-label={t('chat.picture.open')} onClick={() => open(gallery(), index)}>
            <img
              src={pictureSrc(API_BASE_URL, picture.id)}
              alt={prompt}
              width={picture.width}
              height={picture.height}
              onLoad={() => setLoaded(true)}
              // A picture that will not load is not kept waiting under the field.
              onError={() => setLoaded(true)}
            />
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
        </>
      )}
    </div>
  )
}
