import { isOpenChatError, type BrowserProgress } from '@openchat/api-client'
import { AppWindow } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { targetName } from '@openchat/chat-core/browser-tool'
import { useAnswerPluginConfirmation } from '@/hooks/use-plugins'

/**
 * A browser action that changes something on a site, waiting for the person
 * (docs/design/browser-plan.md §4): the site, what would be clicked or sent and
 * why it is asked, and 拒绝 / 这个网站本对话都允许 / 允许. The run waits on it,
 * as a plugin call does; once answered, the card goes and the step shows again.
 */
export default function BrowserConfirmCard({ conversationId, progress }: { conversationId: string; progress: BrowserProgress }) {
  const { t } = useTranslation()
  const answer = useAnswerPluginConfirmation(conversationId)
  const [error, setError] = useState('')
  const site = progress.site ?? ''
  const target = targetName(progress.target)

  const decide = (decision: 'allow' | 'always' | 'deny') => {
    if (!progress.confirmationId) return
    setError('')
    answer.mutate(
      { confirmationId: progress.confirmationId, decision },
      {
        onError: (err) => setError(isOpenChatError(err) && err.code === 'confirmation_gone' ? t('plugins.confirm.gone') : t('plugins.confirm.failed')),
      },
    )
  }

  return (
    <section className="n-plugin-confirm" aria-label={t('browser.confirm.label')}>
      <header className="n-plugin-confirm-head">
        <AppWindow aria-hidden="true" />
        <div>
          <h3 className="n-plugin-confirm-title">{t('browser.confirm.title', { site })}</h3>
          <p className="n-plugin-confirm-hint">{t(`browser.confirm.reason.${progress.reason ?? 'model'}`, { defaultValue: t('browser.confirm.reason.model') })}</p>
        </div>
      </header>
      <dl className="n-plugin-confirm-args">
        <dt>{t('browser.confirm.action')}</dt>
        <dd>
          <pre>{t(`browser.confirm.what.${progress.action}`, { target: target || t('browser.confirm.unnamed'), defaultValue: progress.action })}</pre>
        </dd>
        {progress.url && (
          <>
            <dt>{t('browser.confirm.page')}</dt>
            <dd>
              <pre>{progress.url}</pre>
            </dd>
          </>
        )}
      </dl>
      {error && <p className="n-plugin-confirm-error">{error}</p>}
      <div className="n-plan-actions">
        <button type="button" className="n-plan-btn" disabled={answer.isPending} onClick={() => decide('deny')}>
          {t('plugins.confirm.deny')}
        </button>
        <div className="grow" />
        <button type="button" className="n-plan-btn" disabled={answer.isPending} onClick={() => decide('always')}>
          {t('browser.confirm.always')}
        </button>
        <button type="button" className="n-plan-btn is-primary" disabled={answer.isPending} onClick={() => decide('allow')}>
          {t('plugins.confirm.allow')}
        </button>
      </div>
    </section>
  )
}
