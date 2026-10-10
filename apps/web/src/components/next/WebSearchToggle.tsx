import { Globe } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'
import Tooltip from './Tooltip'

interface WebSearchToggleProps {
  on: boolean
  onChange: (on: boolean) => void
  disabled?: boolean
}

/**
 * Whether this conversation may use the web. On, the model searches and reads
 * pages when a question needs it; off, it answers from what it knows. A switch
 * rather than a menu: there is nothing to choose but on or off.
 */
export function WebSearchToggle({ on, onChange, disabled }: WebSearchToggleProps) {
  const { t } = useTranslation()
  return (
    <Tooltip label={t(on ? 'chat.web-search.on-hint' : 'chat.web-search.off-hint')}>
      <button
        type="button"
        className={cn('n-web-toggle', on && 'is-on')}
        aria-pressed={on}
        aria-label={t('chat.web-search.label')}
        disabled={disabled}
        onClick={() => onChange(!on)}
      >
        <Globe aria-hidden="true" />
      </button>
    </Tooltip>
  )
}

export default WebSearchToggle
