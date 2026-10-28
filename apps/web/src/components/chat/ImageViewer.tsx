import { ChevronLeft, ChevronRight, Download, ExternalLink, X } from 'lucide-react'
import { Dialog } from 'radix-ui'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'
import { useImageViewer } from '@/stores/image-viewer-store'

/**
 * A picture full size over the page: the one clicked, with the others of its
 * group a step away (arrows, ← →, a swipe). The picture fits the window; a
 * click on it shows it at its own size, scrollable, and another fits it again.
 * Esc, the close button or a click beside the picture closes it. Drawn once, by
 * the chat shell, into its own scope (`host`).
 */
export function ImageViewer({ host }: { host: HTMLElement | null }) {
  const { t } = useTranslation()
  const { images, index, close, go } = useImageViewer()
  const image = images[index]
  const many = images.length > 1

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'ArrowLeft' && many) {
      event.preventDefault()
      go(-1)
    } else if (event.key === 'ArrowRight' && many) {
      event.preventDefault()
      go(1)
    }
  }

  return (
    <Dialog.Root open={Boolean(image)} onOpenChange={(open) => { if (!open) close() }}>
      <Dialog.Portal container={host}>
        <Dialog.Overlay className="n-viewer-overlay" />
        <Dialog.Content
          className="n-viewer"
          aria-describedby={undefined}
          onKeyDown={onKeyDown}
          // A click on the dark around the picture closes it, as one outside a dialog does.
          onClick={(event) => { if (event.target === event.currentTarget) close() }}
        >
          {image && (
            <>
              <header className="n-viewer-bar">
                <Dialog.Title className="n-viewer-title" dir="auto">{image.title}</Dialog.Title>
                {many && <span className="n-viewer-count">{t('viewer.count', { index: index + 1, total: images.length })}</span>}
                <div className="n-viewer-actions">
                  {image.download && (
                    <button type="button" className="n-viewer-btn" aria-label={t('viewer.download')} title={t('viewer.download')} onClick={image.download}>
                      <Download />
                    </button>
                  )}
                  <a
                    className="n-viewer-btn"
                    href={image.link ?? image.src}
                    target="_blank"
                    rel="noreferrer"
                    aria-label={image.link ? t('viewer.source') : t('viewer.original')}
                    title={image.link ? t('viewer.source') : t('viewer.original')}
                  >
                    <ExternalLink />
                  </a>
                  <Dialog.Close className="n-viewer-btn" aria-label={t('viewer.close')} title={t('viewer.close')}>
                    <X />
                  </Dialog.Close>
                </div>
              </header>

              {/* Keyed by the picture: each opens fitted, with its own chance to load. */}
              <Stage key={image.src} src={image.src} title={image.title} onClose={close} onSwipe={many ? go : undefined} />

              {many && (
                <>
                  <button type="button" className="n-viewer-nav is-prev" aria-label={t('viewer.previous')} onClick={() => go(-1)}>
                    <ChevronLeft />
                  </button>
                  <button type="button" className="n-viewer-nav is-next" aria-label={t('viewer.next')} onClick={() => go(1)}>
                    <ChevronRight />
                  </button>
                </>
              )}
            </>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

/** The picture itself: fitted, or at its own size; a swipe steps on when fitted. */
function Stage({ src, title, onClose, onSwipe }: { src: string; title: string; onClose: () => void; onSwipe?: (step: 1 | -1) => void }) {
  const { t } = useTranslation()
  const [actual, setActual] = useState(false)
  const [failed, setFailed] = useState(false)
  const touch = useRef<number | null>(null)

  return (
    <div
      className={cn('n-viewer-stage', actual && 'is-actual')}
      onClick={(event) => { if (event.target === event.currentTarget) onClose() }}
      onTouchStart={(event) => { touch.current = event.touches[0]?.clientX ?? null }}
      onTouchEnd={(event) => {
        const start = touch.current
        const end = event.changedTouches[0]?.clientX
        touch.current = null
        if (!onSwipe || actual || start === null || end === undefined) return
        if (Math.abs(end - start) > 50) onSwipe(end < start ? 1 : -1)
      }}
    >
      {failed ? (
        <p className="n-viewer-failed">{t('viewer.failed')}</p>
      ) : (
        <img
          className="n-viewer-image"
          src={src}
          alt={title}
          draggable={false}
          onError={() => setFailed(true)}
          onClick={() => setActual((value) => !value)}
          title={actual ? t('viewer.fit') : t('viewer.actual')}
        />
      )}
    </div>
  )
}

export default ImageViewer
