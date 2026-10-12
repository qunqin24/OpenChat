import { ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { useIsGuest } from '@/lib/guest/mode'
import type { Refusal } from '@/lib/moderation-refusal'

/**
 * A message content moderation refused, shown where it would have gone: what
 * happened, what to do (change the prompt), and - when more refusals would
 * disable the account or ban the guest - how many are left (only while the page
 * that was refused is open: the warning comes with the response and is not
 * kept). It is the body of a reply (`RefusedReply`), where the model's answer
 * would be, drawn after the refused message each time it is shown, and is
 * written like one.
 */
export default function RefusalCard({ refusal }: { refusal: Refusal }) {
  const { t } = useTranslation()
  const guest = useIsGuest()
  // Written as a reply is: a status line like the tool activity's ("searched
  // 16 times"), then plain text in the body's type. No frame, no fill.
  return (
    <div className="n-refusal" role="alert">
      <p className="n-refusal-head">
        <ShieldAlert className="n-refusal-icon" aria-hidden="true" />
        <span>{t('chat.refused.title')}</span>
        <span className="n-refusal-tag">{t('chat.refused.tag')}</span>
      </p>
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
  )
}
