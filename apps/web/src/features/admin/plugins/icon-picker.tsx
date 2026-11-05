import { ChevronsUpDown, Loader2, X } from 'lucide-react'
import { useState } from 'react'
import type { SimpleIcon } from 'simple-icons'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/shadcn/ui/button'
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from '@/components/shadcn/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/shadcn/ui/popover'

/** One Simple Icons icon, as the picker gives it. */
export interface BrandIcon {
  slug: string
  title: string
  /** Without the #. */
  hex: string
  path: string
}

/** How many matches the list shows: the set has thousands. */
const SHOWN = 60

// The set is about 5 MB of paths: loaded the first time the picker opens, in
// a chunk of its own, and never by the pages people use.
let loading: Promise<BrandIcon[]> | null = null
function loadIcons(): Promise<BrandIcon[]> {
  loading ??= import('simple-icons').then((mod) =>
    (Object.values(mod) as unknown[])
      .filter((v): v is SimpleIcon => typeof v === 'object' && v !== null && 'path' in v && 'slug' in v)
      .map(({ slug, title, hex, path }) => ({ slug, title, hex, path })),
  )
  return loading
}

/**
 * A brand icon from Simple Icons (simpleicons.org, CC0), searched by name. The
 * pick hands back the icon's path and colour, which the plugin keeps, so the
 * pages that draw it need nothing of the set.
 */
export function IconPicker({ slug, onPick }: { slug: string; onPick: (icon: BrandIcon | null) => void }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [icons, setIcons] = useState<BrandIcon[] | null>(null)
  const [query, setQuery] = useState('')

  const openPicker = (next: boolean) => {
    setOpen(next)
    if (next && !icons) void loadIcons().then(setIcons)
  }
  const q = query.trim().toLowerCase()
  const matches = icons
    ? icons
        .filter((icon) => !q || icon.title.toLowerCase().includes(q) || icon.slug.includes(q))
        .sort((a, b) => Number(!a.title.toLowerCase().startsWith(q)) - Number(!b.title.toLowerCase().startsWith(q)))
        .slice(0, SHOWN)
    : []

  return (
    <div className="flex gap-2">
      {/* Modal: the picker sits in the plugin's Sheet, whose scroll lock
          swallows the wheel over anything outside it, the list portalled to
          <body> included; a modal popover is a scroll layer of its own. */}
      <Popover open={open} onOpenChange={openPicker} modal>
        <PopoverTrigger asChild>
          <Button type="button" variant="outline" className="flex-1 justify-between font-normal">
            <span className={slug ? '' : 'text-muted-foreground'}>{slug || t('admin.plugins.icon-none')}</span>
            <ChevronsUpDown className="opacity-50" aria-hidden="true" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-80 p-0" align="start">
          <Command shouldFilter={false}>
            <CommandInput placeholder={t('admin.plugins.icon-search')} value={query} onValueChange={setQuery} />
            <CommandList>
              {!icons ? (
                <div className="text-muted-foreground flex items-center justify-center gap-2 py-6 text-sm">
                  <Loader2 className="size-4 animate-spin" aria-hidden="true" />
                  {t('admin.plugins.icon-loading')}
                </div>
              ) : (
                <>
                  <CommandEmpty>{t('admin.plugins.icon-empty')}</CommandEmpty>
                  {matches.map((icon) => (
                    <CommandItem
                      key={icon.slug}
                      value={icon.slug}
                      onSelect={() => {
                        onPick(icon)
                        setOpen(false)
                      }}
                    >
                      <svg viewBox="0 0 24 24" className="size-4 shrink-0" fill={`#${icon.hex}`} aria-hidden="true">
                        <path d={icon.path} />
                      </svg>
                      <span className="truncate">{icon.title}</span>
                      <span className="text-muted-foreground ms-auto text-xs">{icon.slug}</span>
                    </CommandItem>
                  ))}
                </>
              )}
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
      {slug && (
        <Button type="button" variant="ghost" size="icon" aria-label={t('admin.plugins.icon-clear')} onClick={() => onPick(null)}>
          <X />
        </Button>
      )}
    </div>
  )
}
