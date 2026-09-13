import { ChevronDownIcon } from '@radix-ui/react-icons'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { fonts } from '@/config/fonts'
import { supportedLanguages } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { useFont } from '@/features/admin/context/font-provider'
import { useTheme } from '@/features/admin/context/theme-provider'
import { Button, buttonVariants } from '@/components/shadcn/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/shadcn/ui/form'
import { RadioGroup, RadioGroupItem } from '@/components/shadcn/ui/radio-group'
import { ContentSection } from '../components/content-section'

/**
 * Theme and font as upstream has them, plus the language — which upstream has
 * no need for and this app has nowhere else. The config drawer covers sidebar
 * and layout, so those are not repeated here.
 *
 * Nothing is persisted server-side: the theme lives in the ui-store, the font
 * in a cookie, the language in localStorage through i18next's detector. They
 * are preferences of this browser, not of the account.
 */
export function SettingsAppearance() {
  const { t, i18n } = useTranslation()
  const { font, setFont } = useFont()
  const { theme, setTheme } = useTheme()

  const schema = z.object({
    theme: z.enum(['light', 'dark', 'system']),
    font: z.enum(fonts),
    language: z.enum(supportedLanguages),
  })

  const form = useForm({
    resolver: zodResolver(schema),
    values: {
      theme,
      font,
      // i18next reports region-tagged values like `zh-CN`; the select only
      // knows the base languages the app ships.
      language: supportedLanguages.find((code) => i18n.language.startsWith(code)) ?? 'en',
    },
  })

  const submit = (values: z.output<typeof schema>) => {
    if (values.font !== font) setFont(values.font)
    if (values.theme !== theme) setTheme(values.theme)
    if (!i18n.language.startsWith(values.language)) void i18n.changeLanguage(values.language)
    toast.success(t('admin.settings.saved'))
  }

  const themes = ['light', 'dark', 'system'] as const

  return (
    <ContentSection
      title={t('admin.settings.appearance.title')}
      desc={t('admin.settings.appearance.desc')}
    >
      <Form {...form}>
        <form className="space-y-8" onSubmit={form.handleSubmit(submit)}>
          <FormField
            control={form.control}
            name="language"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.settings.appearance.language')}</FormLabel>
                <div className="relative w-max">
                  <FormControl>
                    <select
                      className={cn(
                        buttonVariants({ variant: 'outline' }),
                        'w-50 appearance-none font-normal',
                        'dark:bg-background dark:hover:bg-background',
                      )}
                      {...field}
                    >
                      {supportedLanguages.map((code) => (
                        <option key={code} value={code}>
                          {t(`admin.settings.appearance.lang-${code}` as 'admin.settings.appearance.lang-zh')}
                        </option>
                      ))}
                    </select>
                  </FormControl>
                  <ChevronDownIcon className="absolute inset-e-3 top-2.5 h-4 w-4 opacity-50" />
                </div>
                <FormDescription>{t('admin.settings.appearance.language-hint')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="font"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.settings.appearance.font')}</FormLabel>
                <div className="relative w-max">
                  <FormControl>
                    <select
                      className={cn(
                        buttonVariants({ variant: 'outline' }),
                        'w-50 appearance-none font-normal capitalize',
                        'dark:bg-background dark:hover:bg-background',
                      )}
                      {...field}
                    >
                      {fonts.map((name) => (
                        <option key={name} value={name}>
                          {name}
                        </option>
                      ))}
                    </select>
                  </FormControl>
                  <ChevronDownIcon className="absolute inset-e-3 top-2.5 h-4 w-4 opacity-50" />
                </div>
                <FormDescription>{t('admin.settings.appearance.font-hint')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="theme"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.ui.menu.theme')}</FormLabel>
                <FormDescription>{t('admin.settings.appearance.theme-hint')}</FormDescription>
                <FormMessage />
                <RadioGroup
                  value={field.value}
                  onValueChange={field.onChange}
                  className="grid max-w-md grid-cols-3 gap-4 pt-2"
                >
                  {themes.map((name) => (
                    <FormItem key={name}>
                      <FormLabel className="[&:has([data-state=checked])>div]:border-primary block cursor-pointer">
                        <FormControl>
                          <RadioGroupItem value={name} className="sr-only" />
                        </FormControl>
                        <ThemePreview variant={name} />
                        <span className="block w-full p-2 text-center font-normal">
                          {t(`admin.ui.menu.${name}` as 'admin.ui.menu.light')}
                        </span>
                      </FormLabel>
                    </FormItem>
                  ))}
                </RadioGroup>
              </FormItem>
            )}
          />

          <Button type="submit">{t('admin.settings.save')}</Button>
        </form>
      </Form>
    </ContentSection>
  )
}

/**
 * Upstream hardcodes hex values so the swatch shows each theme regardless of
 * the one in force. Same trick here, with a third tile: 'system' is drawn as
 * the two halves it chooses between.
 */
function ThemePreview({ variant }: { variant: 'light' | 'dark' | 'system' }) {
  const light = (
    <div className="space-y-2 bg-[#ecedef] p-2">
      <div className="space-y-2 rounded-md bg-white p-2 shadow-xs">
        <div className="h-2 w-[80%] rounded-lg bg-[#ecedef]" />
        <div className="h-2 w-full rounded-lg bg-[#ecedef]" />
      </div>
      <div className="flex items-center space-x-2 rounded-md bg-white p-2 shadow-xs">
        <div className="h-4 w-4 rounded-full bg-[#ecedef]" />
        <div className="h-2 w-full rounded-lg bg-[#ecedef]" />
      </div>
    </div>
  )

  const dark = (
    <div className="space-y-2 bg-slate-950 p-2">
      <div className="space-y-2 rounded-md bg-slate-800 p-2 shadow-xs">
        <div className="h-2 w-[80%] rounded-lg bg-slate-400" />
        <div className="h-2 w-full rounded-lg bg-slate-400" />
      </div>
      <div className="flex items-center space-x-2 rounded-md bg-slate-800 p-2 shadow-xs">
        <div className="h-4 w-4 rounded-full bg-slate-400" />
        <div className="h-2 w-full rounded-lg bg-slate-400" />
      </div>
    </div>
  )

  return (
    <div className="border-muted hover:border-accent items-center rounded-md border-2 p-1">
      {variant === 'system' ? (
        <div className="grid grid-cols-2 overflow-hidden rounded-sm">
          <div className="overflow-hidden">{light}</div>
          <div className="overflow-hidden">{dark}</div>
        </div>
      ) : (
        <div className="overflow-hidden rounded-sm">{variant === 'light' ? light : dark}</div>
      )}
    </div>
  )
}

export default SettingsAppearance
