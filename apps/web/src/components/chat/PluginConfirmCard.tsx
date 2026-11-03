import { isOpenChatError, type PluginBadge, type PluginCallProgress } from '@openchat/api-client'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useAnswerPluginConfirmation } from '@/hooks/use-plugins'
import PluginMark from './PluginMark'

/** How many characters of one argument show before it folds. */
const FOLD_AT = 280

interface PluginConfirmCardProps {
  conversationId: string
  call: PluginCallProgress
  plugin?: PluginBadge
}

/**
 * A plugin call that waits for the person (docs/design/plugins-plan.md §6): the
 * plugin, the tool, what it would send, and 拒绝 / 本对话都允许 / 允许. The
 * script that made it stays paused in its run until then; once answered, the
 * card goes and the call shows as a step again.
 */
export default function PluginConfirmCard({ conversationId, call, plugin }: PluginConfirmCardProps) {
  const { t } = useTranslation()
  const answer = useAnswerPluginConfirmation(conversationId)
  const [error, setError] = useState('')
  const name = plugin?.name ?? call.plugin
  const entries = Object.entries(call.arguments ?? {})

  const decide = (decision: 'allow' | 'always' | 'deny') => {
    if (!call.confirmationId) return
    setError('')
    answer.mutate(
      { confirmationId: call.confirmationId, decision },
      {
        onError: (err) => setError(isOpenChatError(err) && err.code === 'confirmation_gone' ? t('plugins.confirm.gone') : t('plugins.confirm.failed')),
      },
    )
  }

  return (
    <section className="n-plugin-confirm" aria-label={t('plugins.confirm.label')}>
      <header className="n-plugin-confirm-head">
        {plugin && <PluginMark plugin={plugin} size={28} />}
        <div>
          <h3 className="n-plugin-confirm-title">{t('plugins.confirm.title', { plugin: name, tool: call.tool })}</h3>
          <p className="n-plugin-confirm-hint">{t('plugins.confirm.hint', { plugin: name })}</p>
        </div>
      </header>
      {entries.length === 0 ? (
        <p className="n-plugin-confirm-none">{t('plugins.confirm.no-arguments')}</p>
      ) : (
        <dl className="n-plugin-confirm-args">
          {entries.map(([key, value]) => (
            <Argument key={key} name={key} value={value} />
          ))}
        </dl>
      )}
      {error && <p className="n-plugin-confirm-error">{error}</p>}
      <div className="n-plan-actions">
        <button type="button" className="n-plan-btn" disabled={answer.isPending} onClick={() => decide('deny')}>
          {t('plugins.confirm.deny')}
        </button>
        <div className="grow" />
        <button type="button" className="n-plan-btn" disabled={answer.isPending} onClick={() => decide('always')}>
          {t('plugins.confirm.always')}
        </button>
        <button type="button" className="n-plan-btn is-primary" disabled={answer.isPending} onClick={() => decide('allow')}>
          {t('plugins.confirm.allow')}
        </button>
      </div>
    </section>
  )
}

/** One argument: text as it is, anything else as JSON; long ones fold. */
function Argument({ name, value }: { name: string; value: unknown }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const text = typeof value === 'string' ? value : JSON.stringify(value, null, 2)
  const long = text.length > FOLD_AT
  return (
    <>
      <dt>{name}</dt>
      <dd>
        <pre className={typeof value === 'string' ? undefined : 'is-json'}>{long && !open ? `${text.slice(0, FOLD_AT)}…` : text}</pre>
        {long && (
          <button type="button" className="n-plugin-confirm-more" onClick={() => setOpen(!open)}>
            {open ? t('plugins.confirm.less') : t('plugins.confirm.more')}
          </button>
        )}
      </dd>
    </>
  )
}
