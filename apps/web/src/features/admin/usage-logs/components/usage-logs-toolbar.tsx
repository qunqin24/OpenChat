import { Cross2Icon, PlusCircledIcon } from '@radix-ui/react-icons'
import { type Table } from '@tanstack/react-table'
import { CheckIcon, RefreshCw } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { ModelIcon } from '@/components/model-icon'
import { DataTableViewOptions } from '@/components/shadcn/data-table/view-options'
import { Button } from '@/components/shadcn/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/shadcn/ui/command'
import { Input } from '@/components/shadcn/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/shadcn/ui/popover'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/shadcn/ui/select'
import { Separator } from '@/components/shadcn/ui/separator'
import { UserAvatar } from '@/components/user-avatar'
import { useAdminChannels, useAdminUsers } from '@/features/admin/use-admin'
import { useChatModels } from '@/hooks/use-chat-models'
import { cn } from '@/lib/utils'
import type { UsageFilters, UsageKind, UsageStatus } from '../use-usage-logs'

/** Radix Select cannot hold an empty value, so "no constraint" gets a name. */
const ANY = '__any__'

export type PickedUser = { id: string; label: string }

export function UsageLogsToolbar<TData>({
  table,
  filters,
  pickedUser,
  onChange,
  onPickUser,
  onReset,
  onRefresh,
  refreshing,
}: {
  table: Table<TData>
  filters: UsageFilters
  /** The chosen user's name, kept beside the id so the button can show it. */
  pickedUser?: PickedUser
  onChange: (patch: Partial<UsageFilters>) => void
  onPickUser: (user?: PickedUser) => void
  onReset: () => void
  onRefresh: () => void
  refreshing: boolean
}) {
  const { t } = useTranslation()
  const { data: channels } = useAdminChannels()

  const filtered = Object.values(filters).some(Boolean)

  return (
    <div className="flex flex-wrap items-center gap-2">
      <UserFilter picked={pickedUser} onPick={onPickUser} />
      <ModelFilter value={filters.model} onPick={(model) => onChange({ model })} />

      <Select
        value={filters.channelId ?? ANY}
        onValueChange={(value) => onChange({ channelId: value === ANY ? undefined : value })}
      >
        <SelectTrigger size="sm" className="w-40" aria-label={t('admin.usage.channel')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ANY}>{t('admin.usage.any-channel')}</SelectItem>
          {(channels ?? []).map((channel) => (
            <SelectItem key={channel.id} value={channel.id}>
              {channel.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Select
        value={filters.status ?? ANY}
        onValueChange={(value) => onChange({ status: value === ANY ? undefined : (value as UsageStatus) })}
      >
        <SelectTrigger size="sm" className="w-32" aria-label={t('admin.usage.status')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ANY}>{t('admin.usage.any-status')}</SelectItem>
          <SelectItem value="ok">{t('admin.usage.status-ok')}</SelectItem>
          <SelectItem value="error">{t('admin.usage.status-error')}</SelectItem>
        </SelectContent>
      </Select>

      <Select
        value={filters.kind ?? ANY}
        onValueChange={(value) => onChange({ kind: value === ANY ? undefined : (value as UsageKind) })}
      >
        <SelectTrigger size="sm" className="w-32" aria-label={t('admin.usage.kind')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ANY}>{t('admin.usage.any-kind')}</SelectItem>
          <SelectItem value="chat">{t('admin.usage.kind-chat')}</SelectItem>
          <SelectItem value="title">{t('admin.usage.kind-title')}</SelectItem>
          <SelectItem value="compression">{t('admin.usage.kind-compression')}</SelectItem>
        </SelectContent>
      </Select>

      <div className="flex items-center gap-1.5">
        <Input
          type="date"
          aria-label={t('admin.usage.from')}
          value={filters.from ?? ''}
          max={filters.to}
          onChange={(event) => onChange({ from: event.target.value || undefined })}
          className="h-8 w-36"
        />
        <span className="text-muted-foreground text-sm">–</span>
        <Input
          type="date"
          aria-label={t('admin.usage.to')}
          value={filters.to ?? ''}
          min={filters.from}
          onChange={(event) => onChange({ to: event.target.value || undefined })}
          className="h-8 w-36"
        />
      </div>

      {filtered && (
        <Button variant="ghost" onClick={onReset} className="h-8 px-2 lg:px-3">
          {t('admin.ui.table.reset')}
          <Cross2Icon className="ms-2 h-4 w-4" />
        </Button>
      )}

      <div className="ms-auto flex items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          className="h-8"
          onClick={onRefresh}
          aria-label={t('admin.usage.refresh')}
        >
          {/* A loading indicator, so it keeps moving under reduced motion. */}
          <RefreshCw className={cn('size-4', refreshing && 'animate-spin')} data-motion="essential" />
          <span className="hidden sm:inline">{t('admin.usage.refresh')}</span>
        </Button>
        <DataTableViewOptions table={table} />
      </div>
    </div>
  )
}

/** The look of the two searchable pickers: a dashed button that names the choice. */
function pickerButton(title: string, value?: ReactNode) {
  return (
    <Button variant="outline" size="sm" className="h-8 border-dashed">
      <PlusCircledIcon className="size-4" />
      {title}
      {value && (
        <>
          <Separator orientation="vertical" className="mx-2 h-4" />
          <span className="max-w-32 truncate font-normal">{value}</span>
        </>
      )}
    </Button>
  )
}

function UserFilter({
  picked,
  onPick,
}: {
  picked?: PickedUser
  onPick: (user?: PickedUser) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  // Searched on the server, like the users page: an installation may have more
  // accounts than a dropdown should hold.
  const { data } = useAdminUsers({ page: 1, search, limit: 20 })

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        {pickerButton(t('admin.usage.user'), picked?.label)}
      </PopoverTrigger>
      <PopoverContent className="w-64 p-0" align="start">
        <Command shouldFilter={false}>
          <CommandInput
            placeholder={t('admin.users.search')}
            value={search}
            onValueChange={setSearch}
          />
          <CommandList>
            <CommandEmpty>{t('admin.ui.table.no-results')}</CommandEmpty>
            <CommandGroup>
              {(data?.items ?? []).map((user) => (
                <CommandItem
                  key={user.id}
                  value={user.id}
                  onSelect={() => {
                    onPick(picked?.id === user.id ? undefined : { id: user.id, label: user.name })
                    setOpen(false)
                  }}
                >
                  <UserAvatar id={user.id} name={user.name} email={user.email} src={user.avatarUrl} className="size-5 text-[10px]" />
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate">{user.name}</span>
                    <span className="text-muted-foreground truncate text-xs">{user.email}</span>
                  </span>
                  {picked?.id === user.id && <CheckIcon className="ms-auto size-4" />}
                </CommandItem>
              ))}
            </CommandGroup>
            {picked && (
              <CommandGroup>
                <CommandItem
                  onSelect={() => {
                    onPick(undefined)
                    setOpen(false)
                  }}
                  className="justify-center text-center"
                >
                  {t('admin.ui.table.clear-filters')}
                </CommandItem>
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

function ModelFilter({ value, onPick }: { value?: string; onPick: (model?: string) => void }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const { data: models } = useChatModels()

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        {pickerButton(t('admin.usage.model'), value)}
      </PopoverTrigger>
      <PopoverContent className="w-72 p-0" align="start">
        <Command>
          <CommandInput placeholder={t('admin.usage.search-model')} />
          <CommandList>
            <CommandEmpty>{t('admin.ui.table.no-results')}</CommandEmpty>
            <CommandGroup>
              {(models ?? []).map((model) => (
                <CommandItem
                  key={model.id}
                  value={`${model.id} ${model.name}`}
                  onSelect={() => {
                    onPick(value === model.id ? undefined : model.id)
                    setOpen(false)
                  }}
                >
                  <ModelIcon
                    subject="model"
                    providerId={model.providerId}
                    family={model.family || model.id}
                    name={model.name}
                    className="size-5"
                  />
                  <span className="truncate">{model.id}</span>
                  {value === model.id && <CheckIcon className="ms-auto size-4" />}
                </CommandItem>
              ))}
            </CommandGroup>
            {value && (
              <CommandGroup>
                <CommandItem
                  onSelect={() => {
                    onPick(undefined)
                    setOpen(false)
                  }}
                  className="justify-center text-center"
                >
                  {t('admin.ui.table.clear-filters')}
                </CommandItem>
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
