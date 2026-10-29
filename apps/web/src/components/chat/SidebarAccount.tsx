import { useNavigate } from '@tanstack/react-router'
import { Check, Languages, LogOut, Monitor, Moon, Settings, Sun, UserRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel,
  DropdownMenuSeparator, DropdownMenuSub, DropdownMenuSubContent,
  DropdownMenuSubTrigger, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { UserAvatar } from '@/components/user-avatar'
import { useSession, useSignOut } from '@/features/auth/use-auth'
import { supportedLanguages } from '@/lib/i18n'
import { useSettings } from '@/lib/settings'
import { useUIStore, type Theme } from '@/stores/ui-store'

const THEMES: Array<{ value: Theme; icon: typeof Sun }> = [
  { value: 'light', icon: Sun },
  { value: 'dark', icon: Moon },
  { value: 'system', icon: Monitor },
]
const LANGUAGE_LABELS = { en: 'English', zh: '中文' }

export function SidebarAccount({ collapsed, onNavigate }: { collapsed: boolean; onNavigate: () => void }) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const { data: session } = useSession()
  const signOut = useSignOut()
  const settings = useSettings()
  const theme = useUIStore((s) => s.theme)
  const setTheme = useUIStore((s) => s.setTheme)
  const ThemeIcon = THEMES.find((option) => option.value === theme)?.icon ?? Monitor

  if (!session) return null
  const name = session.name || session.email

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button type="button" className="n-row n-account-trigger" aria-label={name} title={collapsed ? name : undefined}>
          <UserAvatar id={session.id} name={session.name} email={session.email} src={session.avatarUrl} className="size-[22px] text-[11px]" />
          <span className="n-row-label">{name}</span>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className={collapsed ? 'n-account-menu-rail' : 'n-account-menu'} align="end" side="top">
        <DropdownMenuLabel>
          <UserAvatar id={session.id} name={session.name} email={session.email} src={session.avatarUrl} className="size-[18px] text-[9px]" />
          <span className="n-menu-label-text">{name}</span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => { onNavigate(); settings.open('general') }}>
          <Settings aria-hidden="true" />
          <span>{t('nav.settings')}</span>
        </DropdownMenuItem>
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <ThemeIcon aria-hidden="true" />
            <span>{t('nav.toggleTheme')}</span>
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent>
            {THEMES.map(({ value, icon: Icon }) => (
              <DropdownMenuItem key={value} onSelect={() => setTheme(value)}>
                <Icon aria-hidden="true" />
                <span>{t(`chat.theme.${value}`)}</span>
                {theme === value && <Check className="n-menu-check" aria-hidden="true" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuSubContent>
        </DropdownMenuSub>
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <Languages aria-hidden="true" />
            <span>{t('nav.language')}</span>
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent>
            {supportedLanguages.map((lang) => (
              <DropdownMenuItem key={lang} onSelect={() => void i18n.changeLanguage(lang)}>
                <span>{LANGUAGE_LABELS[lang]}</span>
                {i18n.resolvedLanguage === lang && <Check className="n-menu-check" aria-hidden="true" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuSubContent>
        </DropdownMenuSub>
        <DropdownMenuSeparator />
        {session.role === 'admin' && (
          <DropdownMenuItem onSelect={() => { onNavigate(); void navigate({ to: '/admin' }) }}>
            <UserRound aria-hidden="true" />
            <span>{t('admin.title')}</span>
          </DropdownMenuItem>
        )}
        <DropdownMenuItem onSelect={() => {
          onNavigate()
          void signOut()
        }}>
          <LogOut aria-hidden="true" />
          <span>{t('auth.logout')}</span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
