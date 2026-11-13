import type { components } from '@openchat/api-client'
import { Check, ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { usePersonalization, useUpdatePersonalization } from '@/hooks/use-memory'

type PluginCallsMode = components['schemas']['PluginCallsMode']

const MODES = { ask: 'settings.permissions.ask', allow: 'settings.permissions.allow' } as const satisfies Record<PluginCallsMode, string>

/**
 * What the person allows without being asked. For now one row: a plugin tool
 * an administrator set to ask first either shows its card each time, or runs
 * (Personalization.pluginCalls). Laid out like the general tab's language row,
 * and saved as soon as it is picked.
 */
export function PermissionsTab() {
  const { t } = useTranslation()
  const personalization = usePersonalization()
  const update = useUpdatePersonalization()
  const mode = personalization.data?.pluginCalls ?? 'ask'

  const pick = async (next: PluginCallsMode) => {
    if (next === mode) return
    try {
      await update.mutateAsync({ pluginCalls: next })
    } catch {
      toast.error(t('settings.personalization.save-failed'))
    }
  }

  return (
    <div className="n-settings-tab-body" id="tab-permissions">
      <div className="n-settings-scroll">
        <div className="n-settings-subheading">{t('settings.permissions.plugins')}</div>
        <div className="n-settings-line">
          <span className="n-settings-small-label" id="settings-plugin-calls">{t('settings.permissions.plugin-calls')}</span>
          <DropdownMenu>
            <DropdownMenuTrigger
              className="n-settings-menu-button"
              disabled={!personalization.data || update.isPending}
              aria-labelledby="settings-plugin-calls settings-plugin-calls-value"
            >
              <span id="settings-plugin-calls-value">{t(MODES[mode])}</span>
              <ChevronDown aria-hidden="true" />
            </DropdownMenuTrigger>
            {/* Over the settings dialog, which stands above the page's menus. */}
            <DropdownMenuContent className="n-menu-over-dialog" align="end">
              {(Object.keys(MODES) as PluginCallsMode[]).map((value) => (
                <DropdownMenuItem key={value} onSelect={() => void pick(value)}>
                  <span>{t(MODES[value])}</span>
                  {mode === value && <Check className="n-menu-check" aria-hidden="true" />}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        {mode === 'allow' && <p className="n-settings-help">{t('settings.permissions.allow-note')}</p>}
      </div>
    </div>
  )
}

export default PermissionsTab
