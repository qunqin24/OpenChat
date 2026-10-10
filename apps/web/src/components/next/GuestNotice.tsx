import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

/**
 * The slim notice across the top of a guest's chat: where their conversations
 * are kept (in this browser, and nowhere else), and the way to an account.
 */
export function GuestNotice() {
  const { t } = useTranslation()
  return (
    <div className="n-guest-notice" role="note">
      <span>{t('guest.notice')}</span>
      <Link to="/login" className="n-guest-notice-login">
        {t('guest.sign-in')}
      </Link>
    </div>
  )
}

export default GuestNotice
