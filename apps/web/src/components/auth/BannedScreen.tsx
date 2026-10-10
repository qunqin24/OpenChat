import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

/**
 * What signing in to a disabled account shows: the whole window, styled like a
 * game client's ban notice (dark slate, hard red, heavy type, clipped corners)
 * instead of a line of red text under the form.
 *
 * The server says `account_disabled` only after the password checks out, so
 * this tells nothing to someone guessing, and showing the account it was asked
 * about gives nothing away either. It is the same screen whether the
 * administrator stopped the account or rejected it at approval - both are a
 * disabled account, and an administrator can lift either, so it never says
 * "permanent". "Back" returns to an empty form, so someone else can sign in on
 * the same computer.
 */
export function BannedScreen({ account, onBack }: { account: string; onBack: () => void }) {
  const { t } = useTranslation()
  const back = useRef<HTMLButtonElement>(null)
  const title = t('auth.banned.title')

  useEffect(() => {
    back.current?.focus({ preventScroll: true })
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onBack()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onBack])

  return (
    <div
      className="n-banned"
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="n-banned-title"
      aria-describedby="n-banned-text"
    >
      <div className="n-banned-column">
        <div className="n-banned-kicker" aria-hidden="true">
          <span className="n-banned-kicker-mark" />
          ACCOUNT SUSPENDED
        </div>

        {/* The two copies behind the title are the red/cyan split of the
            entrance glitch; data-text feeds them. */}
        <h1 id="n-banned-title" className="n-banned-title" data-text={title}>
          {title}
        </h1>

        <div className="n-banned-rule" aria-hidden="true" />

        <dl className="n-banned-facts">
          <div>
            <dt>{t('auth.banned.account')}</dt>
            <dd>{account}</dd>
          </div>
          <div>
            <dt>{t('auth.banned.status')}</dt>
            <dd className="n-banned-status">{t('auth.banned.status-value')}</dd>
          </div>
          <div>
            <dt>{t('auth.banned.by')}</dt>
            <dd>{t('auth.banned.by-value')}</dd>
          </div>
        </dl>

        <p id="n-banned-text" className="n-banned-text">
          {t('auth.banned.text')}
        </p>

        <button ref={back} type="button" className="n-banned-back" onClick={onBack}>
          {t('auth.banned.back')}
        </button>
      </div>
    </div>
  )
}

export default BannedScreen
