import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { useStorageLost } from '@/lib/guest/kept'

/**
 * The slim notice across the top of a guest's chat: where their conversations
 * are kept (in this browser, and nowhere else), and the way to an account.
 * When the browser would not give the page a place to keep them, it says
 * instead that they are gone on reload.
 */
export function GuestNotice() {
  const { t } = useTranslation()
  const lost = useStorageLost()
  return (
    <div className="n-guest-notice" role="note">
      <span>{lost ? t('guest.notice-unsaved') : t('guest.notice')}</span>
      <Link to="/login" className="n-guest-notice-login">
        {t('guest.sign-in')}
      </Link>
    </div>
  )
}

export default GuestNotice
