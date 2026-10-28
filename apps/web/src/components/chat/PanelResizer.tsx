import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { clampPanelWidth } from '@/lib/panel-width'

/** How far an arrow key moves the divider. */
const STEP = 24

/**
 * The divider between the chat and the panel beside it: drag it (or focus it
 * and use the arrow keys) to give either more room; a double click puts it
 * back. While dragging it writes the width straight onto the page, so the
 * chat does not re-render on every move, and hands it over once on release.
 */
export function PanelResizer({ width, onChange }: { width: number | null; onChange: (width: number | null) => void }) {
  const { t } = useTranslation()
  const ref = useRef<HTMLDivElement>(null)

  const main = () => ref.current?.closest<HTMLElement>('.n-main') ?? null
  const current = () => {
    const panel = main()?.querySelector<HTMLElement>('.n-doc-panel, .n-subagent-panel, .n-file-panel')
    return width ?? panel?.getBoundingClientRect().width ?? 0
  }

  const onPointerDown = (event: React.PointerEvent) => {
    const page = main()
    if (!page || event.button !== 0) return
    event.preventDefault()
    const handle = event.currentTarget as HTMLDivElement
    handle.setPointerCapture(event.pointerId)
    page.classList.add('is-resizing')
    const rect = page.getBoundingClientRect()
    let next = current()
    const move = (e: PointerEvent) => {
      next = clampPanelWidth(rect.right - e.clientX, rect.width)
      page.style.setProperty('--n-doc-user-w', `${next}px`)
    }
    const up = () => {
      handle.removeEventListener('pointermove', move)
      handle.removeEventListener('pointerup', up)
      handle.removeEventListener('pointercancel', up)
      page.classList.remove('is-resizing')
      onChange(next)
    }
    handle.addEventListener('pointermove', move)
    handle.addEventListener('pointerup', up)
    handle.addEventListener('pointercancel', up)
  }

  const onKeyDown = (event: React.KeyboardEvent) => {
    const page = main()
    if (!page || (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight')) return
    event.preventDefault()
    // Left widens the panel: the divider moves left.
    const by = event.key === 'ArrowLeft' ? STEP : -STEP
    onChange(clampPanelWidth(current() + by, page.getBoundingClientRect().width))
  }

  return (
    <div
      ref={ref}
      className="n-panel-resizer"
      role="separator"
      aria-orientation="vertical"
      aria-label={t('chat.panel-resize')}
      aria-valuenow={width ?? undefined}
      tabIndex={0}
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
      onDoubleClick={() => onChange(null)}
    />
  )
}

export default PanelResizer
