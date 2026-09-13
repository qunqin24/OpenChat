import { useNavigate } from '@tanstack/react-router'
import { Check, Languages, LogOut, Monitor, Moon, Sun, UserRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel,
  DropdownMenuSeparator, DropdownMenuSub, DropdownMenuSubContent,
  DropdownMenuSubTrigger, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useLogout, useSession } from '@/features/auth/use-auth'
import { supportedLanguages } from '@/lib/i18n'
import { useUIStore, type Theme } from '@/stores/ui-store'
import ModelAvatar from './ModelAvatar'

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
  const logout = useLogout()
  const theme = useUIStore((s) => s.theme)
  const setTheme = useUIStore((s) => s.setTheme)
  const ThemeIcon = THEMES.find((option) => option.value === theme)?.icon ?? Monitor

  if (!session) return null
  const name = session.name || session.email

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button type="button" className="n-row n-account-trigger" aria-label={name} title={collapsed ? name : undefined}>
          <ModelAvatar name={name} size={22} />
          <span className="n-row-label">{name}</span>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className={collapsed ? 'n-account-menu-rail' : 'n-account-menu'} align="end" side="top">
        <DropdownMenuLabel>
          <ModelAvatar name={name} size={18} />
          <span className="n-menu-label-text">{name}</span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <ThemeIcon aria-hidden="true" />
            <span>{t('nav.toggleTheme')}</span>
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent>
            {THEMES.map(({ value, icon: Icon }) => (
              <DropdownMenuItem key={value} onSelect={() => setTheme(value)}>
                <Icon aria-hidden="true" />
                <span>{t(`next.theme.${value}`)}</span>
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
        <DropdownMenuItem onSelect={async () => {
          await logout.mutateAsync()
          onNavigate()
          await navigate({ to: '/login' })
        }}>
          <LogOut aria-hidden="true" />
          <span>{t('auth.logout')}</span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
