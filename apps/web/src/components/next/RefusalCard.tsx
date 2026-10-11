import { ShieldAlert } from 'lucide-react'
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
      <ShieldAlert className="n-refusal-icon" aria-hidden="true" />
      <div className="n-refusal-body">
        <p className="n-refusal-title">{t('chat.refused.title')}</p>
        <p className="n-refusal-text">{t('chat.refused.text')}</p>
        {refusal.strikesLeft ? (
          <p className="n-refusal-warning">
            {t(guest ? 'chat.refused.warning-guest' : 'chat.refused.warning', {
              count: refusal.strikesLeft,
              hours: refusal.windowHours,
            })}
          </p>
        ) : null}
      </div>
    </div>
  )
}
