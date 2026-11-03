import { zodResolver } from '@hookform/resolvers/zod'
import { isOpenChatError } from '@openchat/api-client'
import { AlertTriangle, Loader2, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { PasswordInput } from '@/components/shadcn/password-input'
import { ProfileDropdown } from '@/components/shadcn/profile-dropdown'
import { Search } from '@/components/shadcn/search'
import { Alert, AlertDescription, AlertTitle } from '@/components/shadcn/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/shadcn/ui/alert-dialog'
import { Badge } from '@/components/shadcn/ui/badge'
import { Button } from '@/components/shadcn/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/shadcn/ui/form'
import { Input } from '@/components/shadcn/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/shadcn/ui/select'
import { Separator } from '@/components/shadcn/ui/separator'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/shadcn/ui/sheet'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Switch } from '@/components/shadcn/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/shadcn/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/shadcn/ui/tabs'
import { Textarea } from '@/components/shadcn/ui/textarea'
import { glyphColor } from '@/lib/plugin-icon'
import {
  useAdminPlugins,
  useDeletePlugin,
  usePluginTools,
  useRefreshPluginTools,
  useSavePlugin,
  useSetToolPermission,
  type AdminPlugin,
  type AdminPluginInput,
  type PluginToolPermission,
} from './use-admin-plugins'
import { IconPicker, type BrandIcon } from './icon-picker'

/**
 * Plugins (后台 → 插件, docs/design/plugins-plan.md): what people can install
 * from their plugins page. Each is a name and a mark, an MCP service (its
 * address, and how it is reached: no account, a header set here for everyone,
 * or each person's own account), and the service's tools, each allowed,
 * asked about before each call, or blocked. The header is write-only and kept
 * sealed with OPENCHAT_SECRET_KEY, without which it cannot be stored.
 */
export function AdminPlugins() {
  const { t } = useTranslation()
  const plugins = useAdminPlugins()
  const save = useSavePlugin()
  const remove = useDeletePlugin()
  // undefined: closed; null: a new one; a plugin: that one.
  const [editing, setEditing] = useState<AdminPlugin | null | undefined>(undefined)
  const [deleting, setDeleting] = useState<AdminPlugin | null>(null)

  const items = plugins.data?.items ?? []

  const toggle = (plugin: AdminPlugin, enabled: boolean) =>
    save.mutate(
      { id: plugin.id, body: { ...inputOf(plugin), enabled } },
      {
        onSuccess: () => toast.success(t(enabled ? 'admin.plugins.turned-on' : 'admin.plugins.turned-off', { name: plugin.name })),
        onError: () => toast.error(t('admin.plugins.save-failed')),
      },
    )

  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
        <ProfileDropdown />
      </Header>

      <Main className="flex flex-1 flex-col">
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h2 className="text-2xl font-bold tracking-tight">{t('admin.plugins.title')}</h2>
            <p className="text-muted-foreground">{t('admin.plugins.subtitle')}</p>
          </div>
          <Button onClick={() => setEditing(null)}>
            <Plus />
            {t('admin.plugins.new')}
          </Button>
        </div>
        <Separator className="my-4 lg:my-6" />

        {plugins.data && !plugins.data.secretKeySet && (
          <Alert className="mb-4">
            <AlertTriangle aria-hidden="true" />
            <AlertTitle>{t('admin.plugins.no-key')}</AlertTitle>
            <AlertDescription>{t('admin.plugins.no-key-hint')}</AlertDescription>
          </Alert>
        )}

        {plugins.isPending ? (
          <Skeleton className="h-48 w-full" />
        ) : plugins.isError ? (
          <p className="text-muted-foreground text-sm">{t('admin.plugins.load-failed')}</p>
        ) : items.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t('admin.plugins.empty')}</p>
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('admin.plugins.name')}</TableHead>
                  <TableHead>{t('admin.plugins.service')}</TableHead>
                  <TableHead className="w-28">{t('admin.plugins.auth')}</TableHead>
                  <TableHead className="w-24">{t('admin.plugins.tools')}</TableHead>
                  <TableHead className="w-24">{t('admin.plugins.installs')}</TableHead>
                  <TableHead className="w-20">{t('admin.plugins.enabled')}</TableHead>
                  <TableHead className="w-24" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((plugin) => (
                  <TableRow key={plugin.id}>
                    <TableCell>
                      <div className="flex items-center gap-3">
                        <Mark plugin={plugin} />
                        <div className="min-w-0">
                          <div className="flex items-center gap-2 font-medium">
                            {plugin.name}
                            {plugin.builtin && <Badge variant="outline">{t('admin.plugins.builtin')}</Badge>}
                            {plugin.featured && <Badge variant="secondary">{t('admin.plugins.featured')}</Badge>}
                            {plugin.audience === 'admins' && <Badge variant="outline">{t('admin.plugins.admins-only')}</Badge>}
                          </div>
                          <div className="text-muted-foreground truncate text-xs">
                            {plugin.slug}
                            {plugin.category && ` · ${plugin.category}`}
                          </div>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className="text-muted-foreground max-w-[16rem] truncate text-sm">
                      {plugin.mcpUrl ? hostOf(plugin.mcpUrl) : t('admin.plugins.skills-only')}
                    </TableCell>
                    <TableCell className="text-sm">{t(`admin.plugins.auth-${plugin.authType}`)}</TableCell>
                    <TableCell className="text-sm">{plugin.tools > 0 ? `${plugin.toolsOn} / ${plugin.tools}` : '—'}</TableCell>
                    <TableCell className="text-sm">{plugin.installs}</TableCell>
                    <TableCell>
                      <Switch
                        checked={plugin.enabled}
                        disabled={save.isPending}
                        aria-label={t('admin.plugins.enabled')}
                        onCheckedChange={(enabled) => toggle(plugin, enabled)}
                      />
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-1">
                        <Button variant="ghost" size="icon" aria-label={t('admin.plugins.edit')} onClick={() => setEditing(plugin)}>
                          <Pencil />
                        </Button>
                        {/* One that ships with OpenChat is turned off, not deleted. */}
                        {!plugin.builtin && (
                          <Button variant="ghost" size="icon" aria-label={t('admin.plugins.delete')} onClick={() => setDeleting(plugin)}>
                            <Trash2 />
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </Main>

      <Sheet open={editing !== undefined} onOpenChange={(open) => !open && setEditing(undefined)}>
        <SheetContent className="w-full overflow-y-auto sm:max-w-lg">
          {editing !== undefined && (
            <PluginEditor
              // A new one becomes the saved one in place: the tools tab opens up.
              key={editing?.id ?? 'new'}
              plugin={editing}
              secretKeySet={plugins.data?.secretKeySet ?? false}
              onSaved={(saved) => setEditing(saved)}
            />
          )}
        </SheetContent>
      </Sheet>

      <AlertDialog open={Boolean(deleting)} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('admin.plugins.delete-title', { name: deleting?.name })}</AlertDialogTitle>
            <AlertDialogDescription>{t('admin.plugins.delete-body', { count: deleting?.installs ?? 0 })}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('admin.plugins.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                deleting &&
                remove.mutate(deleting.id, {
                  onSuccess: () => toast.success(t('admin.plugins.deleted', { name: deleting.name })),
                  onError: () => toast.error(t('admin.plugins.save-failed')),
                })
              }
            >
              {t('admin.plugins.delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

export default AdminPlugins

function Mark({ plugin }: { plugin: Pick<AdminPlugin, 'iconText' | 'iconColor' | 'iconPath' | 'name'> }) {
  return (
    <span
      aria-hidden="true"
      className="bg-foreground text-background grid size-9 shrink-0 place-items-center rounded-lg text-sm font-semibold"
      style={plugin.iconColor ? { background: plugin.iconColor, color: glyphColor(plugin.iconColor) } : undefined}
    >
      {plugin.iconPath ? (
        <svg viewBox="0 0 24 24" className="size-5" fill="currentColor">
          <path d={plugin.iconPath} />
        </svg>
      ) : (
        plugin.iconText || plugin.name.slice(0, 1)
      )}
    </span>
  )
}

function hostOf(address: string): string {
  try {
    return new URL(address).host
  } catch {
    return address
  }
}

/** A plugin as the form sends it back, the header's value left as it is. */
function inputOf(plugin: AdminPlugin): AdminPluginInput {
  return {
    slug: plugin.slug,
    name: plugin.name,
    description: plugin.description,
    about: plugin.about,
    category: plugin.category,
    iconText: plugin.iconText,
    iconColor: plugin.iconColor,
    iconSlug: plugin.iconSlug,
    iconPath: plugin.iconPath,
    featured: plugin.featured,
    enabled: plugin.enabled,
    audience: plugin.audience,
    mcpUrl: plugin.mcpUrl,
    authType: plugin.authType,
    headerName: plugin.headerName,
    // Sent back as they are, so switching it on or off keeps its OAuth client.
    oauthClientId: plugin.oauthRegistered ? '' : plugin.oauthClientId,
    oauthScopes: plugin.oauthScopes,
    sort: plugin.sort,
  }
}

function PluginEditor({ plugin, secretKeySet, onSaved }: { plugin: AdminPlugin | null; secretKeySet: boolean; onSaved: (p: AdminPlugin) => void }) {
  const { t } = useTranslation()
  return (
    <>
      <SheetHeader>
        <SheetTitle>{plugin ? plugin.name : t('admin.plugins.new')}</SheetTitle>
        <SheetDescription>{t('admin.plugins.editor-hint')}</SheetDescription>
      </SheetHeader>
      <Tabs defaultValue="settings" className="px-4 pb-6">
        <TabsList>
          <TabsTrigger value="settings">{t('admin.plugins.tab-settings')}</TabsTrigger>
          <TabsTrigger value="tools" disabled={!plugin}>
            {t('admin.plugins.tab-tools')}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="settings" className="pt-4">
          <SettingsForm plugin={plugin} secretKeySet={secretKeySet} onSaved={onSaved} />
        </TabsContent>
        {plugin && (
          <TabsContent value="tools" className="pt-4">
            <ToolsPanel plugin={plugin} />
          </TabsContent>
        )}
      </Tabs>
    </>
  )
}

function SettingsForm({ plugin, secretKeySet, onSaved }: { plugin: AdminPlugin | null; secretKeySet: boolean; onSaved: (p: AdminPlugin) => void }) {
  const { t } = useTranslation()
  const save = useSavePlugin()

  const schema = z.object({
    name: z.string().trim().min(1, t('admin.plugins.required')).max(60),
    slug: z
      .string()
      .trim()
      .regex(/^[a-z0-9]+(-[a-z0-9]+)*$/, t('admin.plugins.slug-rule'))
      .max(32, t('admin.plugins.slug-rule')),
    description: z.string().max(200),
    about: z.string().max(2000),
    category: z.string().max(30),
    iconText: z.string().max(4),
    iconColor: z.string().max(30),
    iconSlug: z.string(),
    iconPath: z.string(),
    featured: z.boolean(),
    audience: z.enum(['all', 'admins']),
    mcpUrl: z
      .string()
      .trim()
      .refine((v) => v === '' || /^https?:\/\/\S+$/.test(v), t('admin.plugins.url-rule')),
    authType: z.enum(['none', 'header', 'oauth']),
    headerName: z.string().trim().max(100),
    headerValue: z.string(),
    oauthClientId: z.string().trim().max(300),
    oauthClientSecret: z.string(),
    oauthScopes: z.string().trim().max(1000),
    sort: z.string().trim().regex(/^-?\d*$/, t('admin.plugins.number')),
  })
  type Values = z.infer<typeof schema>

  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: plugin?.name ?? '',
      slug: plugin?.slug ?? '',
      description: plugin?.description ?? '',
      about: plugin?.about ?? '',
      category: plugin?.category ?? '',
      iconText: plugin?.iconText ?? '',
      iconColor: plugin?.iconColor ?? '',
      iconSlug: plugin?.iconSlug ?? '',
      iconPath: plugin?.iconPath ?? '',
      featured: plugin?.featured ?? false,
      audience: plugin?.audience ?? 'all',
      mcpUrl: plugin?.mcpUrl ?? '',
      authType: plugin?.authType ?? 'none',
      headerName: plugin?.headerName || 'Authorization',
      headerValue: '',
      // One registered on the fly is the server's, not a field to edit.
      oauthClientId: plugin?.oauthRegistered ? '' : (plugin?.oauthClientId ?? ''),
      oauthClientSecret: '',
      oauthScopes: plugin?.oauthScopes ?? '',
      sort: String(plugin?.sort ?? 0),
    },
  })
  const authType = useWatch({ control: form.control, name: 'authType' })
  // Where the service sends people back: what its OAuth app is registered with.
  const callbackURL = `${window.location.origin}/api/v1/plugins/oauth/callback`
  const preview = useWatch({ control: form.control, name: ['iconText', 'iconColor', 'name', 'iconPath', 'iconSlug'] })
  // A picked icon brings its brand colour; clearing it keeps the colour.
  const pickIcon = (icon: BrandIcon | null) => {
    form.setValue('iconSlug', icon?.slug ?? '', { shouldDirty: true })
    form.setValue('iconPath', icon?.path ?? '', { shouldDirty: true })
    if (icon) form.setValue('iconColor', `#${icon.hex}`, { shouldDirty: true })
  }

  const submit = (values: Values) => {
    const body: AdminPluginInput = {
      ...values,
      enabled: plugin?.enabled ?? true,
      sort: Number(values.sort || 0),
      headerValue: undefined,
    }
    // The header's value goes only when typed: empty keeps the stored one.
    if (values.authType === 'header' && values.headerValue.trim()) body.headerValue = values.headerValue.trim()
    // The client secret goes only when typed: empty keeps the stored one.
    body.oauthClientSecret = values.authType === 'oauth' && values.oauthClientSecret.trim() ? values.oauthClientSecret.trim() : undefined
    if (values.authType !== 'header' && plugin?.headerSet) body.headerValue = ''
    save.mutate(
      { id: plugin?.id, body },
      {
        onSuccess: (saved) => {
          toast.success(t('admin.plugins.saved'))
          form.reset({ ...values, headerValue: '', oauthClientSecret: '' })
          onSaved(saved)
        },
        onError: (error) => {
          const code = isOpenChatError(error) ? error.code : ''
          if (code === 'slug_taken') form.setError('slug', { message: t('admin.plugins.slug-taken') })
          else if (code === 'secret_key_missing') toast.error(t('admin.plugins.no-key'))
          else toast.error(t('admin.plugins.save-failed'))
        },
      },
    )
  }

  const text = (name: 'name' | 'slug' | 'description' | 'category' | 'iconText' | 'iconColor' | 'mcpUrl' | 'headerName' | 'sort', label: string, hint?: string, placeholder?: string) => (
    <FormField
      control={form.control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input {...field} placeholder={placeholder} />
          </FormControl>
          {hint && <FormDescription>{hint}</FormDescription>}
          <FormMessage />
        </FormItem>
      )}
    />
  )

  return (
    <Form {...form}>
      <form className="space-y-5" onSubmit={form.handleSubmit(submit)}>
        {/* The mark beside the icon picker, level with its box; the colour,
            and the letters drawn when there is no icon, under it. */}
        <div className="flex items-end gap-3">
          <Mark plugin={{ iconText: preview[0], iconColor: preview[1], name: preview[2] || '?', iconPath: preview[3] }} />
          <div className="grid flex-1 gap-2">
            <FormLabel>{t('admin.plugins.icon')}</FormLabel>
            <IconPicker slug={preview[4]} onPick={pickIcon} />
          </div>
        </div>
        <div className="grid grid-cols-2 items-start gap-3">
          {text('iconColor', t('admin.plugins.icon-color'), undefined, '#181717')}
          {text('iconText', t('admin.plugins.icon-text'), t('admin.plugins.icon-text-hint'), 'GH')}
        </div>
        <p className="text-muted-foreground -mt-2 text-xs">{t('admin.plugins.icon-credit')}</p>
        {text('name', t('admin.plugins.name'))}
        {text('slug', t('admin.plugins.slug'), t('admin.plugins.slug-hint'), 'github')}
        {text('description', t('admin.plugins.description'), t('admin.plugins.description-hint'))}
        <FormField
          control={form.control}
          name="about"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('admin.plugins.about')}</FormLabel>
              <FormControl>
                <Textarea rows={3} {...field} />
              </FormControl>
              <FormDescription>{t('admin.plugins.about-hint')}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <div className="grid grid-cols-2 items-start gap-3">
          {text('category', t('admin.plugins.category'), undefined, t('admin.plugins.category-placeholder'))}
          {text('sort', t('admin.plugins.sort'), t('admin.plugins.sort-hint'))}
        </div>
        <div className="grid grid-cols-2 items-start gap-3">
          <FormField
            control={form.control}
            name="audience"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.plugins.audience')}</FormLabel>
                <Select value={field.value} onValueChange={field.onChange}>
                  <FormControl>
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value="all">{t('admin.plugins.audience-all')}</SelectItem>
                    <SelectItem value="admins">{t('admin.plugins.audience-admins')}</SelectItem>
                  </SelectContent>
                </Select>
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="featured"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.plugins.featured')}</FormLabel>
                <div className="flex h-9 items-center">
                  <FormControl>
                    <Switch checked={field.value} onCheckedChange={field.onChange} />
                  </FormControl>
                </div>
              </FormItem>
            )}
          />
        </div>

        <Separator />
        <div>
          <h3 className="text-sm font-medium">{t('admin.plugins.service-section')}</h3>
          <p className="text-muted-foreground text-sm">{t('admin.plugins.service-section-hint')}</p>
        </div>
        {text('mcpUrl', t('admin.plugins.mcp-url'), t('admin.plugins.mcp-url-hint'), 'https://mcp.example.com/mcp')}
        <FormField
          control={form.control}
          name="authType"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('admin.plugins.auth')}</FormLabel>
              <Select value={field.value} onValueChange={field.onChange}>
                <FormControl>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  <SelectItem value="none">{t('admin.plugins.auth-none')}</SelectItem>
                  <SelectItem value="header">{t('admin.plugins.auth-header')}</SelectItem>
                  <SelectItem value="oauth">{t('admin.plugins.auth-oauth')}</SelectItem>
                </SelectContent>
              </Select>
              <FormDescription>{t(`admin.plugins.auth-${authType}-hint`)}</FormDescription>
            </FormItem>
          )}
        />
        {authType === 'header' && (
          <div className="grid grid-cols-[1fr_2fr] items-start gap-3">
            {text('headerName', t('admin.plugins.header-name'), undefined, 'Authorization')}
            <FormField
              control={form.control}
              name="headerValue"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('admin.plugins.header-value')}</FormLabel>
                  <FormControl>
                    <PasswordInput
                      autoComplete="off"
                      disabled={!secretKeySet}
                      placeholder={plugin?.headerSet ? t('admin.plugins.header-kept') : 'Bearer …'}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>{secretKeySet ? t('admin.plugins.header-hint') : t('admin.plugins.no-key')}</FormDescription>
                </FormItem>
              )}
            />
          </div>
        )}

        {authType === 'oauth' && (
          <div className="space-y-4">
            <FormItem>
              <FormLabel>{t('admin.plugins.oauth-callback')}</FormLabel>
              <div className="flex gap-2">
                <Input readOnly value={callbackURL} className="font-mono text-xs" onFocus={(e) => e.currentTarget.select()} />
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => void navigator.clipboard.writeText(callbackURL).then(() => toast.success(t('admin.plugins.copied')))}
                >
                  {t('admin.plugins.copy')}
                </Button>
              </div>
              <FormDescription>{t('admin.plugins.oauth-callback-hint')}</FormDescription>
            </FormItem>
            <FormField
              control={form.control}
              name="oauthClientId"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('admin.plugins.oauth-client-id')}</FormLabel>
                  <FormControl>
                    <Input autoComplete="off" spellCheck={false} {...field} placeholder={plugin?.oauthRegistered ? t('admin.plugins.oauth-registered') : ''} />
                  </FormControl>
                  <FormDescription>{t('admin.plugins.oauth-client-id-hint')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="oauthClientSecret"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('admin.plugins.oauth-client-secret')}</FormLabel>
                  <FormControl>
                    <PasswordInput
                      autoComplete="off"
                      disabled={!secretKeySet}
                      placeholder={plugin?.oauthSecretSet ? t('admin.plugins.header-kept') : ''}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>{secretKeySet ? t('admin.plugins.header-hint') : t('admin.plugins.no-key')}</FormDescription>
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="oauthScopes"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('admin.plugins.oauth-scopes')}</FormLabel>
                  <FormControl>
                    <Input autoComplete="off" spellCheck={false} className="font-mono text-sm" {...field} />
                  </FormControl>
                  <FormDescription>{t('admin.plugins.oauth-scopes-hint')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        )}

        <div className="flex justify-end">
          <Button type="submit" disabled={save.isPending}>
            {save.isPending && <Loader2 className="animate-spin" aria-hidden="true" />}
            {plugin ? t('admin.plugins.save') : t('admin.plugins.create')}
          </Button>
        </div>
      </form>
    </Form>
  )
}

function ToolsPanel({ plugin }: { plugin: AdminPlugin }) {
  const { t, i18n } = useTranslation()
  const tools = usePluginTools(plugin.id)
  const refresh = useRefreshPluginTools(plugin.id)
  const setPermission = useSetToolPermission(plugin.id)
  const items = tools.data?.items ?? []
  const fetched = tools.data?.fetchedAt
  const canList = Boolean(plugin.mcpUrl)

  const list = () =>
    refresh.mutate(undefined, {
      onSuccess: (result) => toast.success(t('admin.plugins.tools-listed', { count: result.items.length })),
      onError: (error) =>
        toast.error(
          isOpenChatError(error) && error.code === 'plugin_not_connected'
            ? t('admin.plugins.tools-connect-first')
            : isOpenChatError(error) && error.message
              ? error.message
              : t('admin.plugins.tools-failed'),
        ),
    })

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <p className="text-muted-foreground text-sm">
          {!plugin.mcpUrl
            ? t('admin.plugins.tools-no-service')
            : plugin.authType === 'oauth'
              ? t('admin.plugins.tools-oauth')
              : fetched
                ? t('admin.plugins.tools-fetched', {
                    when: new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(fetched)),
                  })
                : t('admin.plugins.tools-never')}
        </p>
        <Button variant="outline" size="sm" disabled={!canList || refresh.isPending} onClick={list}>
          {refresh.isPending ? <Loader2 className="animate-spin" aria-hidden="true" /> : <RefreshCw aria-hidden="true" />}
          {t('admin.plugins.tools-refresh')}
        </Button>
      </div>
      <p className="text-muted-foreground text-xs">{t('admin.plugins.permission-hint')}</p>
      {tools.isPending ? (
        <Skeleton className="h-32 w-full" />
      ) : items.length === 0 ? null : (
        <div className="divide-y rounded-md border">
          {items.map((tool) => (
            <div key={tool.name} className="flex items-start gap-3 p-3">
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <code className="text-sm">{tool.name}</code>
                  {tool.readOnly ? (
                    <Badge variant="secondary">{t('admin.plugins.read-only')}</Badge>
                  ) : tool.destructive ? (
                    <Badge variant="outline">{t('admin.plugins.destructive')}</Badge>
                  ) : null}
                </div>
                {(tool.title || tool.description) && (
                  <p className="text-muted-foreground mt-1 line-clamp-2 text-xs">{tool.title || tool.description}</p>
                )}
              </div>
              <Select
                value={tool.permission}
                disabled={setPermission.isPending}
                onValueChange={(permission) =>
                  setPermission.mutate(
                    { name: tool.name, permission: permission as PluginToolPermission },
                    { onError: () => toast.error(t('admin.plugins.save-failed')) },
                  )
                }
              >
                <SelectTrigger className="w-32" aria-label={t('admin.plugins.permission')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="allow">{t('admin.plugins.permission-allow')}</SelectItem>
                  <SelectItem value="confirm">{t('admin.plugins.permission-confirm')}</SelectItem>
                  <SelectItem value="blocked">{t('admin.plugins.permission-blocked')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
