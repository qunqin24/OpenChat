import { Telescope } from 'lucide-react'
import { useTranslation } from 'react-i18next'

/**
 * Fills whatever it is placed in, rather than claiming the viewport.
 *
 * Upstream sizes this `h-svh`, which suits a standalone error page but not a
 * panel route: SidebarInset is already full height and adds its own margin in
 * the inset variant, so a full-viewport child overflows by exactly that margin
 * and leaves the page scrollable by a few pixels.
 */
export function ComingSoon() {
  const { t } = useTranslation()

  return (
    <div className="flex h-full flex-1 flex-col items-center justify-center gap-2">
      <Telescope size={72} />
      <h1 className="text-4xl leading-tight font-bold">{t('admin.ui.coming-soon.title')}</h1>
      <p className="text-muted-foreground text-center">{t('admin.ui.coming-soon.description')}</p>
    </div>
  )
}
