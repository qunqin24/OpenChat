import type { TraceRequest } from '@openchat/api-client'
import { useQuery } from '@tanstack/react-query'
import { RotateCw, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { api, unwrap } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import {
  messagesOf,
  responseOf,
  systemOf,
  textOf,
  argumentsOf,
  toolsOf,
  traceFigures,
  traceRows,
  traceSpans,
  type TraceRow,
} from '@/lib/trace'
import { cn } from '@/lib/utils'

/**
 * 轨迹: an administrator's own conversation as the model saw it - every request
 * with what it carried and what came back, on a timeline, with its timing and
 * tokens. Drawn over the transcript (which stays mounted underneath) while it
 * is open. A debugging aid: the server records only administrators' requests
 * and keeps them a week (api/traces.go).
 */
export function TraceView({ conversationId, running }: { conversationId: string; running: boolean }) {
  const { t } = useTranslation()
  const trace = useQuery({
    queryKey: queryKeys.trace(conversationId),
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/conversations/{conversationId}/trace', { params: { path: { conversationId } }, signal })),
    // Follows a run while it goes; a finished conversation is asked once.
    refetchInterval: running ? 2000 : false,
  })
  const requests = useMemo(() => trace.data?.requests ?? [], [trace.data])
  const rows = useMemo(() => traceRows(requests), [requests])
  const { spans, total } = useMemo(() => traceSpans(requests), [requests])
  // A row of the list, or (from the timeline) a request as a whole.
  const [selection, setSelection] = useState<{ request: number; row: string | null } | null>(null)
  const selected = selection?.request ?? null
  const [search, setSearch] = useState('')
  const needle = search.trim().toLowerCase()
  const shown = needle ? rows.filter((row) => `${row.name ?? ''} ${row.text}`.toLowerCase().includes(needle)) : rows

  const totals = useMemo(() => {
    let input = 0, output = 0, cached = 0
    for (const r of requests) {
      input += r.inputTokens + r.cacheReadTokens + r.cacheWriteTokens
      output += r.outputTokens
      cached += r.cacheReadTokens
    }
    return { input, output, hit: input > 0 ? cached / input : null }
  }, [requests])

  const current = selected !== null ? requests[selected] : undefined

  return (
    <div className="n-trace">
      <div className="n-trace-bar">
        <span>{t('trace.requests', { count: requests.length })}</span>
        <span>{t('trace.total', { seconds: seconds(total) })}</span>
        <span>{t('trace.tokens', { input: totals.input, output: totals.output })}</span>
        {totals.hit !== null && <span>{t('trace.cache', { percent: Math.round(totals.hit * 100) })}</span>}
        <span className="n-trace-tools-bar">
          <input
            className="n-trace-search"
            type="search"
            value={search}
            placeholder={t('trace.search')}
            aria-label={t('trace.search')}
            onChange={(event) => setSearch(event.target.value)}
          />
          <button type="button" className="n-icon-btn" aria-label={t('trace.refresh')} onClick={() => void trace.refetch()}>
            <RotateCw className={cn('h-4 w-4', trace.isFetching && 'n-trace-spin')} data-motion="essential" />
          </button>
        </span>
      </div>

      {trace.isPending ? (
        <div className="n-library-state" role="status" aria-label={t('state.loading')}>
          <span className="n-spinner" data-motion="essential" />
        </div>
      ) : trace.isError ? (
        <div className="n-library-state">
          <button type="button" className="n-library-retry" onClick={() => void trace.refetch()}>{t('trace.retry')}</button>
        </div>
      ) : requests.length === 0 ? (
        <div className="n-library-state">
          <p className="n-library-empty-title">{t('trace.empty')}</p>
          <p className="n-library-empty-hint">{t('trace.empty-hint', { days: trace.data?.retentionDays ?? 7 })}</p>
        </div>
      ) : (
        <div className="n-trace-body">
          <div className="n-trace-main">
            <div className="n-trace-timeline" aria-label={t('trace.timeline')}>
              {(['model', 'tool'] as const).map((kind) => (
                <div key={kind} className="n-trace-track">
                  <span className="n-trace-track-label">{t(`trace.track.${kind}`)}</span>
                  <div className="n-trace-lane">
                    {spans.filter((span) => span.kind === kind).map((span, i) => (
                      <button
                        key={i}
                        type="button"
                        className={cn('n-trace-span', `is-${kind}`, span.error && 'is-error', selected === span.request && 'is-selected')}
                        style={{ left: pct(span.start, total), width: `max(3px, ${pct(span.end - span.start, total)})` }}
                        title={`#${span.request + 1} · ${seconds(span.end - span.start)} s`}
                        onClick={() => setSelection({ request: span.request, row: null })}
                      >
                        {span.firstAt !== undefined && (
                          <span className="n-trace-wait" style={{ width: pct(span.firstAt - span.start, span.end - span.start) }} />
                        )}
                      </button>
                    ))}
                  </div>
                </div>
              ))}
            </div>

            <ol className="n-trace-rows">
              {shown.map((row, i) => (
                <Row
                  key={row.key}
                  row={row}
                  first={i === 0 || shown[i - 1]!.request !== row.request}
                  selected={selection?.row === row.key}
                  inRequest={selection !== null && selection.row === null && selection.request === row.request}
                  onSelect={() => setSelection({ request: row.request, row: row.key })}
                />
              ))}
            </ol>
          </div>

          {current && selection && (
            <Detail
              // A new selection opens on its own first tab.
              key={`${selection.request}:${selection.row ?? ''}`}
              request={current}
              index={selection.request}
              row={rows.find((row) => row.key === selection.row)}
              onClose={() => setSelection(null)}
            />
          )}
        </div>
      )}
    </div>
  )
}

function Row({ row, first, selected, inRequest, onSelect }: { row: TraceRow; first: boolean; selected: boolean; inRequest: boolean; onSelect: () => void }) {
  const { t } = useTranslation()
  return (
    <li className={cn('n-trace-row', selected && 'is-selected', inRequest && 'is-in-selected', first && 'is-first')}>
      {first && <span className="n-trace-row-index">#{row.request + 1}</span>}
      <button type="button" className="n-trace-row-button" onClick={onSelect}>
        <span className={cn('n-trace-chip', `is-${row.kind}`)}>{t(`trace.kind.${row.kind}`)}</span>
        {row.name && <span className="n-trace-row-name">{row.name}</span>}
        <span className={cn('n-trace-row-text', (row.kind === 'tool_call' || row.kind === 'tool_result') && 'is-code', row.error && 'is-error')}>
          {oneLine(row.text) || t('trace.empty-text')}
        </span>
      </button>
    </li>
  )
}

type Tab = 'content' | 'overview' | 'request' | 'response' | 'raw'

function Detail({ request, index, row, onClose }: { request: TraceRequest; index: number; row?: TraceRow; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const tabs: Tab[] = row ? ['content', 'overview', 'request', 'response', 'raw'] : ['overview', 'request', 'response', 'raw']
  const [tab, setTab] = useState<Tab>(tabs[0]!)
  const figures = traceFigures(request)
  const response = responseOf(request)
  const tools = toolsOf(request)
  const started = new Date(request.startedAt)
  const time = new Intl.DateTimeFormat(i18n.resolvedLanguage, {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', fractionalSecondDigits: 3, hourCycle: 'h23',
  })

  return (
    <aside className="n-trace-detail" aria-label={t('trace.detail', { index: index + 1 })}>
      <header className="n-trace-detail-head">
        {row ? (
          <span className={cn('n-trace-chip', `is-${row.kind}`)}>{t(`trace.kind.${row.kind}`)}</span>
        ) : (
          <span className="n-trace-chip is-assistant">{t(`trace.request-kind.${request.kind === 'compression' ? 'compression' : 'chat'}`)}</span>
        )}
        <span className="n-trace-detail-title">
          #{index + 1}
          {row?.name && <> · <span className="n-trace-row-name">{row.name}</span></>}
        </span>
        <button type="button" className="n-icon-btn" aria-label={t('settings.close')} onClick={onClose}><X className="h-4 w-4" /></button>
      </header>
      <div className="n-trace-tabs" role="tablist">
        {tabs.map((value) => (
          <button key={value} type="button" role="tab" aria-selected={tab === value} className={cn('n-trace-tab', tab === value && 'active')} onClick={() => setTab(value)}>
            {t(`trace.tab.${value}`)}
          </button>
        ))}
      </div>

      <div className="n-trace-detail-body">
        {tab === 'content' && row && (
          <pre className={cn('n-trace-pre', row.kind !== 'tool_call' && row.kind !== 'tool_result' && 'is-prose', row.error && 'is-error')}>
            {row.kind === 'tool_call' ? prettyJson(row.text) : row.text || t('trace.empty-text')}
          </pre>
        )}

        {tab === 'overview' && (
          <dl className="n-trace-facts">
            <Fact label={t('trace.fact.status')} value={request.status === 'ok' ? t('trace.ok') : `${t('trace.failed')} · ${request.errorCode ?? ''}`} error={request.status === 'error'} />
            {request.errorMessage && <Fact label={t('trace.fact.error')} value={request.errorMessage} error />}
            <Fact label={t('trace.fact.model')} value={request.servedModel && request.servedModel !== request.modelId ? `${request.modelId} → ${request.servedModel}` : request.modelId} />
            <Fact label={t('trace.fact.channel')} value={request.channelName || '—'} />
            <Fact label={t('trace.fact.started')} value={time.format(started)} />
            <Fact label={t('trace.fact.duration')} value={`${seconds(request.durationMs)} s`} />
            <Fact label={t('trace.fact.first-token')} value={request.firstTokenMs != null ? `${seconds(request.firstTokenMs)} s` : '—'} />
            <Fact label={t('trace.fact.generation')} value={figures.generationMs != null ? `${seconds(figures.generationMs)} s` : '—'} />
            <Fact label={t('trace.fact.throughput')} value={figures.throughput != null ? `${figures.throughput.toFixed(1)} tok/s` : '—'} />
            <Fact label={t('trace.fact.input')} value={t('trace.fact.input-value', { total: figures.promptTokens, cached: request.cacheReadTokens, written: request.cacheWriteTokens })} />
            <Fact label={t('trace.fact.cache')} value={figures.cacheHit != null ? `${Math.round(figures.cacheHit * 100)}%` : '—'} />
            <Fact label={t('trace.fact.output')} value={t('trace.fact.output-value', { total: request.outputTokens, reasoning: request.reasoningTokens })} />
            <Fact label={t('trace.fact.cost')} value={request.cost != null ? `$${request.cost.toFixed(6)}` : '—'} />
            {request.estimated && <Fact label="" value={t('trace.estimated')} />}
          </dl>
        )}

        {tab === 'request' && (
          <div className="n-trace-sections">
            <Section title={t('trace.section.system')}><pre className="n-trace-pre">{systemOf(request) || '—'}</pre></Section>
            <Section title={t('trace.section.messages', { count: messagesOf(request).filter((m) => m.role !== 'system').length })}>
              {messagesOf(request).filter((m) => m.role !== 'system').map((message, i) => (
                <div key={i} className="n-trace-message">
                  <span className={cn('n-trace-chip', `is-${message.role === 'tool' ? 'tool_result' : message.role}`)}>{t(`trace.role.${message.role}`, { defaultValue: message.role })}</span>
                  <pre className="n-trace-pre">{describeParts(message.parts)}</pre>
                </div>
              ))}
            </Section>
            <Section title={t('trace.section.tools', { count: tools.length })}>
              <p className="n-trace-tools">{tools.map((tool) => tool.name).join(', ') || '—'}</p>
            </Section>
          </div>
        )}

        {tab === 'response' && (
          <div className="n-trace-sections">
            {response ? <pre className="n-trace-pre">{describeParts(response.parts)}</pre> : <p>—</p>}
            {response && textOf(response) === '' && response.parts.length === 0 && <p>{t('trace.empty-text')}</p>}
          </div>
        )}

        {tab === 'raw' && <pre className="n-trace-pre is-raw">{JSON.stringify({ request: request.request, response: request.response }, null, 2)}</pre>}
      </div>
    </aside>
  )
}

function Fact({ label, value, error }: { label: string; value: string; error?: boolean }) {
  return (
    <>
      <dt>{label}</dt>
      <dd className={cn(error && 'is-error')}>{value}</dd>
    </>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="n-trace-section">
      <h3 className="n-trace-section-title">{title}</h3>
      {children}
    </section>
  )
}

/** A message's parts as text: what each is, then what it says. */
function describeParts(parts: { type: string; text?: string; name?: string; content?: string; error?: string; filename?: string; rawArguments?: string; arguments?: unknown }[]): string {
  return parts
    .map((part) => {
      switch (part.type) {
        case 'text':
          return part.text ?? ''
        case 'reasoning':
          return `〔reasoning〕\n${part.text ?? ''}`
        case 'tool_call':
          return `〔tool_call ${part.name ?? ''}〕 ${argumentsOf(part)}`
        case 'tool_result':
          return `〔tool_result ${part.name ?? ''}〕\n${part.error || part.content || ''}`
        case 'attachment':
          return `〔file ${part.filename ?? ''}〕`
        default:
          return `〔${part.type}〕`
      }
    })
    .join('\n\n')
}

/** A tool call's arguments laid out, when they are JSON. */
function prettyJson(text: string): string {
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
}

const oneLine = (text: string) => text.replace(/\s+/g, ' ').trim()
const seconds = (ms: number) => (ms / 1000).toFixed(2)
const pct = (part: number, whole: number) => `${whole > 0 ? (part / whole) * 100 : 0}%`

export default TraceView
