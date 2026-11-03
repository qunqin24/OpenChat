import type { PluginCallProgress, PluginCodeProgress } from '@openchat/api-client'
import { ChevronRight, Puzzle, Search, TriangleAlert } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { pluginName, pluginProgressOf } from '@openchat/chat-core/plugin-code'
import { isFailure, stepLabel, type ToolActivityStep } from '@openchat/chat-core/tool-activity'
import { API_BASE_URL } from '@/lib/api'
import { cn } from '@/lib/utils'
import PluginMark from './PluginMark'

/**
 * A plugin script (or one call of a directly listed plugin) as a step of the
 * process, after Claude's "Used GitHub": one line - each plugin with the tools
 * it was asked for - and a chevron; opened, a light panel lists each call's
 * request and the start of its response, and switches to the script. Pictures
 * a call answered with show under the line, open or not.
 */
export default function PluginStep({ step }: { step: ToolActivityStep }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [tab, setTab] = useState<'calls' | 'code'>('calls')
  const progress = pluginProgressOf(step.result?.details, step.progress)
  const failed = isFailure(step.status)
  const running = step.status === 'running' || step.status === 'pending'
  const called = progress?.calls.find((call) => call.status !== 'failed' || progress.calls.length === 1)
  const mark = progress?.plugins.find((p) => p.slug === called?.plugin) ?? progress?.plugins[0]
  const lookedUp = progress !== null && progress.calls.length === 0 && (progress.searches?.length ?? 0) > 0
  const canOpen = progress !== null && (progress.calls.length > 0 || Boolean(progress.code) || Boolean(progress.error) || lookedUp)

  return (
    <li className={cn('n-activity-step', failed && 'is-failed')}>
      <span className="n-activity-step-icon">
        {failed ? (
          <TriangleAlert aria-hidden="true" />
        ) : lookedUp ? (
          <Search aria-hidden="true" />
        ) : mark ? (
          <PluginMark plugin={mark} size={16} />
        ) : (
          <Puzzle aria-hidden="true" />
        )}
      </span>
      <div className="n-activity-step-body">
        {canOpen ? (
          <button type="button" className="n-plugin-step-head" aria-expanded={open} onClick={() => setOpen(!open)}>
            <span>{stepLabel(t, step, running)}</span>
            <ChevronRight className={cn('n-plugin-step-chevron', open && 'is-open')} aria-hidden="true" />
          </button>
        ) : (
          <div className="n-activity-step-label">{stepLabel(t, step, running)}</div>
        )}
        {progress?.images && progress.images.length > 0 && step.conversationId && (
          <div className="n-activity-images">
            {progress.images.map((image) => {
              const src = `${API_BASE_URL}/conversations/${encodeURIComponent(step.conversationId ?? '')}/tool-images/${encodeURIComponent(image.id)}`
              return (
                <a key={image.id} href={src} target="_blank" rel="noreferrer">
                  <img src={src} alt={t('plugins.step.image')} loading="lazy" decoding="async" />
                </a>
              )
            })}
          </div>
        )}
        {open && progress && <Details progress={progress} tab={tab} onTab={setTab} />}
      </div>
    </li>
  )
}

function Details({ progress, tab, onTab }: { progress: PluginCodeProgress; tab: 'calls' | 'code'; onTab: (tab: 'calls' | 'code') => void }) {
  const { t } = useTranslation()
  return (
    <div className="n-plugin-detail">
      {tab === 'calls' ? (
        <>
          {progress.searches?.map((search, i) => (
            <div key={`s${i}`} className="n-plugin-call">
              <span className="n-plugin-call-name">search()</span>
              <span className="n-plugin-call-line">{t('plugins.step.search-line', { query: search.query || '*', count: search.found })}</span>
            </div>
          ))}
          {progress.calls.map((call) => (
            <Call key={call.seq} call={call} progress={progress} />
          ))}
          {/* Why the script failed, as the model was told: written for the
              model, in English, but the only account there is. */}
          {progress.error && <p className="n-plugin-call-error">{progress.error}</p>}
        </>
      ) : (
        <pre className="n-plugin-code">{progress.code}</pre>
      )}
      {progress.code && (
        <div className="n-plugin-tabs" role="tablist">
          <button type="button" role="tab" aria-selected={tab === 'calls'} onClick={() => onTab('calls')}>
            {t('plugins.step.tab-calls')}
          </button>
          <button type="button" role="tab" aria-selected={tab === 'code'} onClick={() => onTab('code')}>
            {t('plugins.step.tab-code')}
          </button>
        </div>
      )}
    </div>
  )
}

function Call({ call, progress }: { call: PluginCallProgress; progress: PluginCodeProgress }) {
  const { t } = useTranslation()
  const request = call.arguments && Object.keys(call.arguments).length > 0 ? JSON.stringify(call.arguments) : t('plugins.step.no-arguments')
  return (
    <div className="n-plugin-call">
      <span className="n-plugin-call-name">
        {pluginName(progress, call.plugin)} · {call.tool}
        {call.status !== 'done' && (
          <span className={cn('n-plugin-call-status', (call.status === 'failed' || call.status === 'denied') && 'is-failed')}>{t(`plugins.step.call-${call.status}`)}</span>
        )}
      </span>
      <dl className="n-plugin-call-kv">
        <dt>{t('plugins.step.request')}</dt>
        <dd>{request}</dd>
        {call.result !== undefined && (
          <>
            <dt>{t('plugins.step.response')}</dt>
            <dd>{call.result}</dd>
          </>
        )}
        {call.error && (
          <>
            <dt>{t('plugins.step.error')}</dt>
            <dd className="is-failed">{call.error}</dd>
          </>
        )}
      </dl>
    </div>
  )
}
