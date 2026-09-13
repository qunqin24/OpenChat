import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { SIDEBAR_WIDTH } from '@/stores/ui-store'

interface Props {
  width: number
  onResize: (width: number | null) => void
  onCommit: (width: number) => void
}

export function SidebarResizeHandle({ width, onResize, onCommit }: Props) {
  const { t } = useTranslation()
  const drag = useRef<{ x: number; width: number; current: number } | null>(null)

  return (
    <div
      className="n-sidebar-resize"
      role="separator"
      tabIndex={0}
      aria-label={t('next.sidebar.resize')}
      aria-orientation="vertical"
      aria-controls="chat-sidebar"
      aria-valuenow={Math.round(width)}
      aria-valuemin={SIDEBAR_WIDTH.min}
      aria-valuemax={SIDEBAR_WIDTH.max}
      onDoubleClick={() => onCommit(SIDEBAR_WIDTH.default)}
      onKeyDown={(event) => {
        const step = event.shiftKey ? 40 : 10
        const targets: Record<string, number> = { ArrowLeft: width - step, ArrowRight: width + step, Home: SIDEBAR_WIDTH.min, End: SIDEBAR_WIDTH.max }
        const target = targets[event.key]
        if (target !== undefined) { event.preventDefault(); onCommit(target) }
      }}
      onPointerDown={(event) => {
        if (event.button !== 0) return
        event.preventDefault()
        event.currentTarget.setPointerCapture(event.pointerId)
        drag.current = { x: event.clientX, width, current: width }
        onResize(width)
      }}
      onPointerMove={(event) => {
        if (!drag.current) return
        const next = Math.min(SIDEBAR_WIDTH.max, Math.max(SIDEBAR_WIDTH.min, drag.current.width + event.clientX - drag.current.x))
        drag.current.current = next
        onResize(next)
      }}
      onPointerUp={(event) => {
        if (!drag.current) return
        onCommit(drag.current.current)
        drag.current = null
        onResize(null)
        event.currentTarget.releasePointerCapture(event.pointerId)
      }}
      onLostPointerCapture={() => { drag.current = null; onResize(null) }}
    />
  )
}
