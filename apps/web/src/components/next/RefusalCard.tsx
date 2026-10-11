import { ShieldAlert, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { useIsGuest } from '@/lib/guest/mode'
import type { Refusal } from '@/lib/moderation-refusal'

/**
 * A message content moderation refused, shown where it would have gone: what
 * happened, what to do (change the prompt), and - when more refusals would
 * disable the account or ban the guest - how many are left. The message was
 * never written, so the card is the page's own and goes with the next send.
 * It is the body of a reply (`RefusedReply`), where the model's answer would be.
 */
export default function RefusalCard({ refusal }: { refusal: Refusal }) {
  const { t } = useTranslation()
  const guest = useIsGuest()
  return (
    <div className="n-refusal" role="alert">
      <div className="n-refusal-main">
        <span className="n-refusal-badge" aria-hidden="true">
          <ShieldAlert className="n-refusal-icon" />
        </span>
        <div className="n-refusal-body">
          <p className="n-refusal-title">
            {t('chat.refused.title')}
            <span className="n-refusal-tag">{t('chat.refused.tag')}</span>
          </p>
          <p className="n-refusal-text">{t('chat.refused.text')}</p>
        </div>
      </div>
      {refusal.strikesLeft ? (
        <p className="n-refusal-warning">
          <TriangleAlert className="n-refusal-warning-icon" aria-hidden="true" />
          {t(guest ? 'chat.refused.warning-guest' : 'chat.refused.warning', {
            count: refusal.strikesLeft,
            hours: refusal.windowHours,
          })}
        </p>
      ) : null}
    </div>
  )
}
