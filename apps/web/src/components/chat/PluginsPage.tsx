import { isOpenChatError, type Plugin } from '@openchat/api-client'
import { Check, ExternalLink, Lock, Plus, Puzzle, X } from 'lucide-react'
import { Dialog } from 'radix-ui'
import { useEffect, useState, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useConnectPlugin, useInstallPlugin, usePlugins, useUninstallPlugin } from '@/hooks/use-plugins'
import { cn } from '@/lib/utils'
import { SidebarToggle } from './ChatShell'
import PluginMark from './PluginMark'

/** The page's filters: everything, what is installed, or one of the administrator's categories. */
type Filter = { kind: 'all' } | { kind: 'installed' } | { kind: 'category'; name: string }

/**
 * Plugins (插件, docs/design/plugins-plan.md): what an administrator added, for
 * each person to install. Laid out as a store (design C, 2026-11-03): the
 * featured plugin on top, the rest in three columns under the filters - all,
 * installed, and the categories in use. A plugin opens a dialog that says what
 * it can do before it is installed, and is where it is uninstalled.
 */
export default function PluginsPage() {
  const { t } = useTranslation()
  const plugins = usePlugins()
  const [host, setHost] = useState<HTMLElement | null>(null)
  const [filter, setFilter] = useState<Filter>({ kind: 'all' })
  const [open, setOpen] = useState<string | null>(null)

  const items = plugins.data?.items ?? []
  // The first featured one not yet installed, else the first featured one.
  const featured = items.find((p) => p.featured && !p.installed) ?? items.find((p) => p.featured)
  const categories = [...new Set(items.map((p) => p.category).filter(Boolean))]
  const installedCount = items.filter((p) => p.installed).length
  const shown = items.filter((p) =>
    filter.kind === 'installed' ? p.installed : filter.kind === 'category' ? p.category === filter.name : p !== featured,
  )
  const chosen = items.find((p) => p.id === open)

  // Back from a service's sign-in: say how it went, once, and take the
  // answer out of the address.
  useEffect(() => {
    const query = new URLSearchParams(window.location.search)
    const connected = query.get('connected')
    const failed = query.get('connect_error')
    if (!connected && !failed) return
    if (connected) toast.success(t('plugins.connected-toast'))
    else toast.error(t(`plugins.connect-error.${failed === 'refused' || failed === 'state' || failed === 'key' ? failed : 'other'}`))
    window.history.replaceState(window.history.state, '', window.location.pathname)
  }, [t])

  return (
    <main className="n-main n-plugins" ref={setHost}>
      <div className="n-topbar">
        <SidebarToggle />
      </div>
      <div className="n-scroll">
        <div className="n-plugins-column">
          <header className="n-plugins-head">
            <h1 className="n-library-title">{t('plugins.title')}</h1>
            <p className="n-plugins-sub">{t('plugins.subtitle')}</p>
          </header>

          {plugins.isPending ? (
            <div className="n-library-state" role="status" aria-label={t('state.loading')}>
              <span className="n-spinner" data-motion="essential" />
            </div>
          ) : plugins.isError ? (
            <div className="n-library-state">
              <button type="button" className="n-library-retry" onClick={() => void plugins.refetch()}>{t('plugins.retry')}</button>
            </div>
          ) : items.length === 0 ? (
            <div className="n-tasks-empty">
              <Puzzle className="n-tasks-empty-icon" aria-hidden="true" />
              <p className="n-tasks-empty-title">{t('plugins.empty')}</p>
              <p className="n-tasks-empty-hint">{t('plugins.empty-hint')}</p>
            </div>
          ) : (
            <>
              {featured && filter.kind === 'all' && (
                <section className="n-plugin-hero" style={tint(featured)} aria-label={t('plugins.featured')}>
                  <PluginMark plugin={featured} size={56} />
                  <div className="n-plugin-hero-text">
                    <span className="n-plugin-hero-label">{t('plugins.featured')}</span>
                    <span className="n-plugin-hero-name">{featured.name}</span>
                    <span className="n-plugin-hero-desc">{featured.about || featured.description}</span>
                  </div>
                  <button type="button" className={cn('n-plugin-btn', featured.installed && 'is-done')} onClick={() => setOpen(featured.id)}>
                    {featured.installed ? <Check aria-hidden="true" /> : <Plus aria-hidden="true" />}
                    {featured.installed ? t('plugins.installed') : t('plugins.install')}
                  </button>
                </section>
              )}

              <div className="n-plugin-filters" role="tablist" aria-label={t('plugins.filters')}>
                <FilterTab active={filter.kind === 'all'} onClick={() => setFilter({ kind: 'all' })}>{t('plugins.all')}</FilterTab>
                <FilterTab active={filter.kind === 'installed'} onClick={() => setFilter({ kind: 'installed' })}>
                  {t('plugins.installed-count', { count: installedCount })}
                </FilterTab>
                {categories.map((name) => (
                  <FilterTab key={name} active={filter.kind === 'category' && filter.name === name} onClick={() => setFilter({ kind: 'category', name })}>
                    {name}
                  </FilterTab>
                ))}
              </div>

              {shown.length === 0 ? (
                <p className="n-plugins-none">{filter.kind === 'installed' ? t('plugins.none-installed') : t('plugins.none-here')}</p>
              ) : (
                <ul className="n-plugin-grid">
                  {shown.map((plugin) => (
                    <li key={plugin.id}>
                      <button type="button" className="n-plugin-tile" onClick={() => setOpen(plugin.id)}>
                        <PluginMark plugin={plugin} size={36} />
                        <span className="n-plugin-name">{plugin.name}</span>
                        <span className="n-plugin-desc">{plugin.description}</span>
                        {plugin.installed && (
                          <span className="n-plugin-badge">
                            <Check aria-hidden="true" />
                            {t('plugins.installed')}
                          </span>
                        )}
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
        </div>
      </div>
      <PluginDialog host={host} plugin={chosen} onClose={() => setOpen(null)} />
    </main>
  )
}

function FilterTab({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button type="button" role="tab" aria-selected={active} className={cn('n-plugin-filter', active && 'is-active')} onClick={onClick}>
      {children}
    </button>
  )
}

/** The featured plugin's band, faintly in its colour. */
function tint(plugin: Plugin): CSSProperties | undefined {
  return plugin.iconColor ? ({ ['--n-plugin-tint' as string]: plugin.iconColor } as CSSProperties) : undefined
}

/**
 * What a plugin can do, before it is installed - what it reads, what it may
 * change and that it asks first, the skills it brings, and for one that acts
 * as the person, that it connects their own account - and the way to install
 * or uninstall it.
 */
function PluginDialog({ host, plugin, onClose }: { host: HTMLElement | null; plugin?: Plugin; onClose: () => void }) {
  const { t } = useTranslation()
  const install = useInstallPlugin()
  const connect = useConnectPlugin()
  const uninstall = useUninstallPlugin()
  const busy = install.isPending || uninstall.isPending || connect.isPending
  const account = plugin?.authType === 'oauth'

  // One that acts as the person is installed by connecting their account.
  const doConnect = (p: Plugin) =>
    connect.mutate(p.id, {
      onError: (error) =>
        toast.error(
          isOpenChatError(error) && error.code === 'plugin_oauth_unconfigured'
            ? t('plugins.oauth-unconfigured')
            : isOpenChatError(error) && error.code === 'plugin_unreachable'
              ? t('plugins.unreachable', { name: p.name })
              : t('plugins.error'),
        ),
    })

  const doInstall = (p: Plugin) =>
    install.mutate(p.id, {
      onSuccess: () => {
        toast.success(t('plugins.installed-toast', { name: p.name }))
        onClose()
      },
      onError: (error) =>
        toast.error(isOpenChatError(error) && error.code === 'plugin_needs_account' ? t('plugins.needs-account') : t('plugins.error')),
    })
  const doUninstall = (p: Plugin) =>
    uninstall.mutate(p.id, {
      onSuccess: () => {
        toast.success(t('plugins.uninstalled-toast', { name: p.name }))
        onClose()
      },
      onError: () => toast.error(t('plugins.error')),
    })

  return (
    <Dialog.Root open={Boolean(plugin)} onOpenChange={(next) => !next && onClose()}>
      <Dialog.Portal container={host}>
        <Dialog.Overlay className="n-modal-overlay">
          <Dialog.Content className="n-modal n-plugin-dialog">
            {plugin && (
              <>
                <Dialog.Close className="n-plugin-dialog-close n-icon-btn sm" aria-label={t('plugins.close')}>
                  <X aria-hidden="true" />
                </Dialog.Close>
                <PluginMark plugin={plugin} size={48} />
                <Dialog.Title className="n-plugin-dialog-title">
                  {plugin.installed ? plugin.name : t('plugins.install-title', { name: plugin.name })}
                </Dialog.Title>
                <Dialog.Description className="n-plugin-dialog-about">
                  {account ? t('plugins.account-about', { name: plugin.name }) : plugin.about || plugin.description}
                </Dialog.Description>
                {account && (plugin.about || plugin.description) && <p className="n-plugin-dialog-about">{plugin.about || plugin.description}</p>}
                {plugin.needsReconnect && <p className="n-plugin-dialog-about is-warning">{t('plugins.needs-reconnect', { name: plugin.name })}</p>}
                <ul className="n-plugin-points">
                  {plugin.readTools > 0 && (
                    <li><Check aria-hidden="true" /><span>{t('plugins.point-read', { count: plugin.readTools })}</span></li>
                  )}
                  {plugin.writeTools > 0 && (
                    <li><Check aria-hidden="true" /><span>{t('plugins.point-write', { count: plugin.writeTools })}</span></li>
                  )}
                  {plugin.skills > 0 && (
                    <li><Check aria-hidden="true" /><span>{t('plugins.point-skills', { count: plugin.skills })}</span></li>
                  )}
                  {account && (
                    <li><Lock aria-hidden="true" /><span>{t('plugins.point-token')}</span></li>
                  )}
                </ul>
                <div className="n-plugin-dialog-foot">
                  {plugin.installed ? (
                    <>
                      <button type="button" className="n-plugin-btn is-quiet" disabled={busy} onClick={() => doUninstall(plugin)}>
                        {t('plugins.uninstall')}
                      </button>
                      {account && (
                        <button type="button" className="n-plugin-btn is-quiet" disabled={busy} onClick={() => doConnect(plugin)}>
                          {t('plugins.reconnect')}
                        </button>
                      )}
                      <Dialog.Close className="n-plugin-btn is-primary">{t('plugins.done')}</Dialog.Close>
                    </>
                  ) : (
                    <>
                      <Dialog.Close className="n-plugin-btn is-quiet">{t('plugins.cancel')}</Dialog.Close>
                      <button type="button" className="n-plugin-btn is-primary" disabled={busy} onClick={() => (account ? doConnect(plugin) : doInstall(plugin))}>
                        {account && <ExternalLink aria-hidden="true" />}
                        {account ? t('plugins.authorize', { name: plugin.name }) : t('plugins.install')}
                      </button>
                    </>
                  )}
                </div>
              </>
            )}
          </Dialog.Content>
        </Dialog.Overlay>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
