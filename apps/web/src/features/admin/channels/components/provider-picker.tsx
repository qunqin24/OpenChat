import { Check, ChevronsUpDown, Pencil } from 'lucide-react'
import { useState, type ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'

import { ModelIcon } from '@/components/model-icon'
import { Button } from '@/components/shadcn/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/shadcn/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/shadcn/ui/popover'
import { cn } from '@/lib/utils'
import type { CatalogProvider } from '../use-provider-catalog'

type ProviderPickerProps = {
  /** A models.dev provider id, or an id the catalogue has never heard of. */
  value: string
  onChange: (providerId: string) => void
  providers: CatalogProvider[]
  isPending: boolean
  isError: boolean
  /**
   * Everything else lands on the trigger button, which is how `FormControl`'s
   * `id`, `aria-describedby` and `aria-invalid` reach the thing the label
   * points at.
   */
} & Omit<ComponentProps<typeof Button>, 'value' | 'onChange'>

/**
 * The provider field: a searchable list of the catalogue, with a way out of it.
 *
 * Two things shape this. First, the catalogue is upwards of two hundred
 * entries, so a `Select` is unusable and the list has to be filtered by typing
 * — cmdk matches the id as the item's value and the display name as a keyword,
 * so `anthropic` and `Anthropic` both find the same row.
 *
 * Second, the catalogue is a cache of somebody else's list. It can be empty
 * because nothing has fetched it yet, stale because a provider was added
 * upstream this morning, or simply missing a private deployment's id. So the
 * footer below the list always offers to take whatever is in the search box
 * verbatim, and the trigger falls back to showing a stored id it cannot name.
 * The field must never be the reason a channel cannot be configured.
 */
export function ProviderPicker({
  value,
  onChange,
  providers,
  isPending,
  isError,
  className,
  ...trigger
}: ProviderPickerProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')

  // Undefined for an id the catalogue does not carry, which is a state the
  // whole component is built to survive rather than an error.
  const selected = providers.find((provider) => provider.id === value)
  const typed = search.trim()

  const choose = (providerId: string) => {
    onChange(providerId)
    setOpen(false)
  }

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        // The query belongs to one visit to the list; reopening should show the
        // whole catalogue again rather than the last thing that was hunted for.
        if (!next) {
          setSearch('')
        }
      }}
    >
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          role="combobox"
          className={cn('w-full justify-between px-3 font-normal', className)}
          {...trigger}
        >
          {value ? (
            <span className="flex min-w-0 flex-1 items-center gap-2">
              <ModelIcon providerId={value} name={selected?.name} className="size-5" />
              {/* A known provider reads as its brand; an unknown one has only
                  its id to show, and monospace says that is what it is. */}
              <span className={cn('truncate', !selected && 'font-mono text-sm')}>
                {selected?.name ?? value}
              </span>
            </span>
          ) : (
            <span className="text-muted-foreground">{t('admin.channels.provider-pick')}</span>
          )}
          <ChevronsUpDown className="size-4 shrink-0 opacity-50" aria-hidden="true" />
        </Button>
      </PopoverTrigger>

      <PopoverContent className="w-(--radix-popover-trigger-width) p-0" align="start">
        <Command>
          <CommandInput
            value={search}
            onValueChange={setSearch}
            placeholder={t('admin.channels.provider-search')}
          />
          <CommandList>
            {isPending && (
              <p className="text-muted-foreground py-6 text-center text-sm">{t('state.loading')}</p>
            )}
            {isError && (
              <p className="text-muted-foreground py-6 text-center text-sm">{t('state.error')}</p>
            )}
            {!isPending && !isError && (
              <CommandEmpty className="text-muted-foreground py-6 text-center text-sm">
                {t('admin.ui.command.no-results')}
              </CommandEmpty>
            )}

            {/* A group rather than bare items: cmdk re-parents rows as it sorts
                them by score, and the only two places it knows how to put one
                back are the list itself and a group's item container. An
                unheaded group needs an explicit value to be identified by. */}
            <CommandGroup value="providers">
              {providers.map((provider) => (
                <CommandItem
                  key={provider.id}
                  // The id is the value, so typing it matches; the name rides
                  // along as a keyword so typing that matches too.
                  value={provider.id}
                  keywords={[provider.name]}
                  onSelect={() => choose(provider.id)}
                  className="gap-2"
                >
                  <ModelIcon providerId={provider.id} name={provider.name} className="size-6" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate">{provider.name}</span>
                    {/* Half the catalogue is near-duplicate names — three
                        Alibaba plans, two MiniMax regions — and the id is the
                        only thing that tells them apart. */}
                    <span className="text-muted-foreground block truncate font-mono text-xs">
                      {provider.id}
                    </span>
                  </span>
                  <span className="text-muted-foreground shrink-0 text-xs tabular-nums">
                    {t('admin.channels.provider-models', { n: provider.modelCount })}
                  </span>
                  <Check
                    className={cn('size-4 shrink-0', provider.id !== value && 'opacity-0')}
                    aria-hidden="true"
                  />
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>

        {/* Outside <Command> on purpose: cmdk's root swallows Enter to select
            whichever row is highlighted, so a button nested inside it could not
            be activated from the keyboard. */}
        <div className="border-t p-1">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            // Disabled reads as the instruction it is — there is nothing to
            // take until something has been typed, and the box to type it in is
            // directly above.
            disabled={typed === ''}
            onClick={() => choose(typed)}
            className="w-full justify-start gap-2 font-normal"
          >
            <Pencil className="size-4 shrink-0 opacity-60" aria-hidden="true" />
            <span className="truncate">{t('admin.channels.provider-custom')}</span>
            {typed !== '' && (
              <span className="ms-auto max-w-[50%] truncate font-mono text-xs">{typed}</span>
            )}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}
