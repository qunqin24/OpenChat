import { Languages } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Tooltip } from '@/components/ui/tooltip'
import { supportedLanguages } from '@/lib/i18n'

const labels: Record<string, string> = {
  en: 'English',
  zh: '中文',
}

export function LanguageSwitcher() {
  const { t, i18n } = useTranslation()

  return (
    <DropdownMenu>
      <Tooltip label={t('nav.language')}>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon" aria-label={t('nav.language')}>
            <Languages />
          </Button>
        </DropdownMenuTrigger>
      </Tooltip>
      <DropdownMenuContent>
        {supportedLanguages.map((lang) => (
          <DropdownMenuItem key={lang} onSelect={() => void i18n.changeLanguage(lang)}>
            <span>{labels[lang] ?? lang}</span>
            {i18n.resolvedLanguage === lang && (
              <span className="ml-auto text-xs text-muted-foreground">•</span>
            )}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
