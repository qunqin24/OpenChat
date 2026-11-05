import { AppWindow, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { browserProgressOf } from '@openchat/chat-core/browser-tool'
import { isFailure, stepLabel, type ToolActivityStep } from '@openchat/chat-core/tool-activity'
import { isHttpUrl } from '@openchat/chat-core/urls'
import { API_BASE_URL } from '@/lib/api'
import { cn } from '@/lib/utils'

/**
 * One action of the browser tool as a step of the process: what it did, the
 * page it left the browser on, and the picture when it took one.
 */
export default function BrowserStep({ step }: { step: ToolActivityStep }) {
  const { t } = useTranslation()
  const progress = browserProgressOf(step.result?.details, step.progress)
  const failed = isFailure(step.status)
  const running = step.status === 'running' || step.status === 'pending'
  const page = !running && progress?.url ? { url: progress.url, title: progress.title } : undefined
  const image = progress?.image && step.conversationId
    ? `${API_BASE_URL}/conversations/${encodeURIComponent(step.conversationId)}/tool-images/${encodeURIComponent(progress.image.id)}`
    : undefined

  return (
    <li className={cn('n-activity-step', failed && 'is-failed')}>
      <span className="n-activity-step-icon">{failed ? <TriangleAlert aria-hidden="true" /> : <AppWindow aria-hidden="true" />}</span>
      <div className="n-activity-step-body">
        <div className="n-activity-step-label">{stepLabel(t, step, running)}</div>
        {page &&
          (isHttpUrl(page.url) ? (
            <a className="n-activity-link" href={page.url} target="_blank" rel="noreferrer">
              {page.title || page.url}
            </a>
          ) : (
            <span className="n-activity-link">{page.title || page.url}</span>
          ))}
        {image && (
          <div className="n-activity-images">
            <a href={image} target="_blank" rel="noreferrer">
              <img src={image} alt={t('browser.step.image')} loading="lazy" decoding="async" />
            </a>
          </div>
        )}
        {/* Written for the model, in English, but the only account of what went wrong. */}
        {step.result && failed && <p className="n-activity-error">{step.result.content}</p>}
      </div>
    </li>
  )
}
