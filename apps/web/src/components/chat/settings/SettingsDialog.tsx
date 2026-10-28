import { Brain, Database, Search, SlidersHorizontal, UserRound, X } from 'lucide-react'
import { Dialog, Tabs } from 'radix-ui'
import { lazy, Suspense, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { isSettingsTab, SETTINGS_TABS, useSettings, type SettingsTab } from '@/lib/settings'
import AccountTab from './AccountTab'
import DataTab from './DataTab'
import GeneralTab from './GeneralTab'

// The tab with the most in it is the one fewest people open.
const PersonalizationTab = lazy(() => import('./PersonalizationTab'))

const ICONS: Record<SettingsTab, typeof Brain> = {
  general: SlidersHorizontal,
  personalization: Brain,
  account: UserRound,
  data: Database,
}

/**
 * Settings as a modal over the chat, after Open WebUI's classic one: a wide
 * rounded card, the title and a plain close mark across the top, a column of
 * tabs on the left (with a search that narrows them) and the tab on the right.
 * On a phone the tabs become a row above the content and the search goes.
 * Whether it is open, and on which tab, is the `settings` search parameter
 * (lib/settings.ts), so the memory chip in a reply can link straight to
 * personalization.
 *
 * Portalled into the shell's `.chat-ui` scope rather than <body>, as the
 * search palette is, so the chat's tokens and control styles reach it.
 */
export function SettingsDialog({ host }: { host: HTMLElement | null }) {
  const { t } = useTranslation()
  const { tab, close } = useSettings()
  const content = useRef<HTMLDivElement>(null)

  return (
    <Dialog.Root open={tab !== undefined} onOpenChange={(open) => { if (!open) close() }}>
      <Dialog.Portal container={host}>
        <Dialog.Overlay className="n-modal-overlay">
          <Dialog.Content
            ref={content}
            className="n-modal n-settings"
            aria-describedby={undefined}
            // Not the close mark, which would open the dialog with a ring on it.
            onOpenAutoFocus={(event) => { event.preventDefault(); content.current?.focus() }}
          >
            <Dialog.Title className="sr-only">{t('settings.title')}</Dialog.Title>
            <SettingsBody host={host} />
          </Dialog.Content>
        </Dialog.Overlay>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

/** Mounted only while the dialog is open, so the tab search starts empty each time. */
function SettingsBody({ host }: { host: HTMLElement | null }) {
  const { t } = useTranslation()
  const { tab, select } = useSettings()
  const [query, setQuery] = useState('')

  const needle = query.trim().toLocaleLowerCase()
  const visible = SETTINGS_TABS.filter(
    (id) =>
      !needle ||
      t(`settings.tabs.${id}`).toLocaleLowerCase().includes(needle) ||
      t(`settings.keywords.${id}`).toLocaleLowerCase().split(',').some((word) => word.trim().includes(needle)),
  )

  return (
    <>
      <header className="n-settings-head">
        <h2 className="n-settings-title" aria-hidden="true">{t('settings.title')}</h2>
        <Dialog.Close className="n-settings-close" aria-label={t('settings.close')}>
          <X aria-hidden="true" />
        </Dialog.Close>
      </header>

      <Tabs.Root
        className="n-settings-layout"
        orientation="vertical"
        value={tab ?? 'general'}
        onValueChange={(value) => { if (isSettingsTab(value)) select(value) }}
      >
        <div className="n-settings-nav">
          <label className="n-settings-search">
            <Search aria-hidden="true" />
            <span className="sr-only">{t('settings.search')}</span>
            <input value={query} placeholder={t('settings.search')} onChange={(e) => setQuery(e.target.value)} />
          </label>
          <Tabs.List className="n-settings-tabs" aria-label={t('settings.title')}>
            {visible.map((id) => {
              const Icon = ICONS[id]
              return (
                <Tabs.Trigger key={id} value={id} className="n-settings-tab">
                  <Icon aria-hidden="true" />
                  <span>{t(`settings.tabs.${id}`)}</span>
                </Tabs.Trigger>
              )
            })}
          </Tabs.List>
          {visible.length === 0 && <div className="n-settings-none">{t('settings.no-results')}</div>}
        </div>

        <Tabs.Content value="general" className="n-settings-panel"><GeneralTab /></Tabs.Content>
        <Tabs.Content value="personalization" className="n-settings-panel">
          <Suspense fallback={<div className="n-settings-loading" role="status"><span className="n-spinner" data-motion="essential" /></div>}>
            <PersonalizationTab host={host} />
          </Suspense>
        </Tabs.Content>
        <Tabs.Content value="account" className="n-settings-panel"><AccountTab /></Tabs.Content>
        <Tabs.Content value="data" className="n-settings-panel"><DataTab /></Tabs.Content>
      </Tabs.Root>
    </>
  )
}

export default SettingsDialog
