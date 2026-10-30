import type { User } from '@openchat/api-client'
import { isOpenChatError } from '@openchat/api-client'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { PasswordInput } from '@/components/shadcn/password-input'
import { Button } from '@/components/shadcn/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/shadcn/ui/dialog'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/shadcn/ui/form'
import { Input } from '@/components/shadcn/ui/input'
import { ScrollArea } from '@/components/shadcn/ui/scroll-area'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/shadcn/ui/select'
import { Switch } from '@/components/shadcn/ui/switch'
import { useCreateUser, useUpdateUser } from '@/features/admin/use-admin'
import { MIN_PASSWORD_LENGTH } from '@openchat/chat-core/auth-validation'
import { formatSize } from '@openchat/chat-core/attachments'

const GIB = 1024 ** 3

/**
 * The quota change a save makes, if any: blank gives the account the default
 * again (-1), a number its own in bytes.
 */
function storageChange(gigabytes: string, current: number | undefined): { storageQuotaBytes?: number } {
  if (gigabytes === '') return current === undefined ? {} : { storageQuotaBytes: -1 }
  const bytes = Math.round(Number(gigabytes) * GIB)
  return bytes === current ? {} : { storageQuotaBytes: bytes }
}

type UsersActionDialogProps = {
  /** Absent in create mode. */
  user?: User
  /** The signed-in admin cannot demote or disable themselves. */
  isSelf?: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * Both halves of upstream's users-action-dialog: creating an account and
 * editing one.
 *
 * The field list is shorter than upstream's because the account model is —
 * there is one `name`, no username and no phone number. Password is required
 * when creating and optional when editing, where leaving it blank means "do
 * not touch it"; that is the only behavioural difference between the modes.
 */
export function UsersActionDialog({ user, isSelf, open, onOpenChange }: UsersActionDialogProps) {
  const { t } = useTranslation()
  const isEdit = user !== undefined

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader className="text-start">
          <DialogTitle>{t(isEdit ? 'admin.users.edit' : 'admin.users.create')}</DialogTitle>
          <DialogDescription>
            {t(isEdit ? 'admin.users.edit-hint' : 'admin.users.create-hint')}
          </DialogDescription>
        </DialogHeader>

        <UserForm user={user} isSelf={isSelf} onOpenChange={onOpenChange} />
      </DialogContent>
    </Dialog>
  )
}

/**
 * The fields and the footer, which Radix mounts when the dialog opens.
 *
 * The split is what lets the shell above stay mounted while closed — Radix
 * needs the element present to animate it out — without the form remembering
 * what was typed the last time. Nothing resets it; it simply was not there.
 */
function UserForm({
  user,
  isSelf,
  onOpenChange,
}: Pick<UsersActionDialogProps, 'user' | 'isSelf' | 'onOpenChange'>) {
  const { t } = useTranslation()
  const createUser = useCreateUser()
  const updateUser = useUpdateUser()
  const isEdit = user !== undefined

  // Built here rather than at module scope so the messages are translated, and
  // so the password rule can depend on the mode.
  const schema = z
    .object({
      name: z
        .string()
        .trim()
        .min(1, t('admin.users.form.name-required'))
        .max(80)
        // Sign-in takes a username or an address in one field; a CHECK
        // constraint keeps the two name spaces disjoint.
        .refine((value) => !value.includes('@'), t('admin.users.form.name-invalid')),
      email: z.email(t('admin.users.form.email-invalid')),
      role: z.enum(['admin', 'user']),
      enabled: z.boolean(),
      // Only offered for an account that is waiting; off leaves it waiting.
      approve: z.boolean(),
      // GB, or blank for the instance's default (admin → 文件存储).
      storageGb: z
        .string()
        .trim()
        .refine((value) => value === '' || (Number.isFinite(Number(value)) && Number(value) >= 0), t('admin.users.form.storage-invalid')),
      password: z.string(),
      confirmPassword: z.string(),
    })
    .superRefine(({ password, confirmPassword }, ctx) => {
      if (!isEdit && password === '') {
        ctx.addIssue({
          code: 'custom',
          path: ['password'],
          message: t('admin.users.form.password-required'),
        })
        return
      }
      // An untouched password field in edit mode means "leave it alone", so
      // nothing below applies to it.
      if (password === '') return

      if (password.length < MIN_PASSWORD_LENGTH) {
        ctx.addIssue({
          code: 'custom',
          path: ['password'],
          message: t('admin.users.form.password-short', { n: MIN_PASSWORD_LENGTH }),
        })
      }
      if (password !== confirmPassword) {
        ctx.addIssue({
          code: 'custom',
          path: ['confirmPassword'],
          message: t('admin.users.form.password-mismatch'),
        })
      }
    })

  const form = useForm<z.input<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: user?.name ?? '',
      email: user?.email ?? '',
      role: user?.role ?? 'user',
      enabled: !user?.disabled,
      approve: false,
      storageGb: user?.storageQuotaBytes == null ? '' : String(Number((user.storageQuotaBytes / GIB).toFixed(2))),
      password: '',
      confirmPassword: '',
    },
  })

  const pending = createUser.isPending || updateUser.isPending

  // useWatch rather than form.watch(): the latter returns a fresh function the
  // React compiler refuses to memoise around.
  const password = useWatch({ control: form.control, name: 'password' })

  const submit = async (values: z.output<typeof schema>) => {
    try {
      if (isEdit) {
        const wasEnabled = !user.disabled
        await updateUser.mutateAsync({
          userId: user.id,
          // Only what actually moved, so a no-op save is a no-op request and
          // the password is never resent as its own hash source.
          ...(values.name !== user.name ? { name: values.name } : {}),
          ...(values.email !== user.email ? { email: values.email } : {}),
          ...(values.role !== user.role ? { role: values.role } : {}),
          ...(values.enabled !== wasEnabled ? { disabled: !values.enabled } : {}),
          ...(user.pending && values.approve ? { approved: true } : {}),
          ...(values.password ? { password: values.password } : {}),
          ...storageChange(values.storageGb, user.storageQuotaBytes),
        })
        toast.success(t('admin.users.updated'))
      } else {
        await createUser.mutateAsync({
          name: values.name,
          email: values.email,
          role: values.role,
          password: values.password,
          disabled: !values.enabled,
        })
        toast.success(t('admin.users.created'))
      }
      onOpenChange(false)
    } catch (error) {
      // A duplicate address or username comes back blaming that field;
      // anything else is a refused change (last admin, self-lockout) with no
      // field to point at.
      if (isOpenChatError(error) && (error.field === 'email' || error.field === 'name')) {
        form.setError(error.field, {
          message: t(
            error.field === 'email'
              ? 'admin.users.form.email-taken'
              : 'admin.users.form.name-taken',
          ),
        })
        return
      }
      toast.error(t('admin.users.update-failed'))
    }
  }

  return (
    <>
      <ScrollArea className="-me-4 h-[26rem] w-full py-1 pe-4">
        <Form {...form}>
          <form
            id="users-action-form"
            className="space-y-4 p-0.5"
            onSubmit={form.handleSubmit((values) => void submit(values))}
          >
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem className="grid grid-cols-6 items-center gap-x-4 gap-y-1 space-y-0">
                  <FormLabel className="col-span-2">{t('admin.users.form.name')}</FormLabel>
                  <FormControl>
                    <Input className="col-span-4" autoComplete="off" {...field} />
                  </FormControl>
                  <FormMessage className="col-span-4 col-start-3" />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="email"
              render={({ field }) => (
                <FormItem className="grid grid-cols-6 items-center gap-x-4 gap-y-1 space-y-0">
                  <FormLabel className="col-span-2">{t('admin.users.form.email')}</FormLabel>
                  <FormControl>
                    <Input className="col-span-4" autoComplete="off" {...field} />
                  </FormControl>
                  <FormMessage className="col-span-4 col-start-3" />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="role"
              render={({ field }) => (
                <FormItem className="grid grid-cols-6 items-center gap-x-4 gap-y-1 space-y-0">
                  <FormLabel className="col-span-2">{t('admin.users.role')}</FormLabel>
                  <Select value={field.value} onValueChange={field.onChange}>
                    <FormControl>
                      <SelectTrigger className="col-span-4 w-full">
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectItem value="admin">{t('admin.users.role-admin')}</SelectItem>
                      {/* Demoting yourself would lock you out of this panel. */}
                      <SelectItem value="user" disabled={isSelf}>
                        {t('admin.users.role-user')}
                      </SelectItem>
                    </SelectContent>
                  </Select>
                  <FormMessage className="col-span-4 col-start-3" />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="enabled"
              render={({ field }) => (
                <FormItem className="grid grid-cols-6 items-center gap-x-4 gap-y-1 space-y-0">
                  <FormLabel className="col-span-2">{t('admin.users.status')}</FormLabel>
                  <div className="col-span-4 flex items-center gap-3">
                    <FormControl>
                      <Switch
                        checked={field.value}
                        disabled={isSelf}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                    <span className="text-muted-foreground text-sm">
                      {t(field.value ? 'admin.users.enabled' : 'admin.users.disabled')}
                    </span>
                  </div>
                  <FormDescription className="col-span-4 col-start-3 text-xs">
                    {t(isSelf ? 'admin.users.self-locked' : 'admin.users.enabled-hint')}
                  </FormDescription>
                </FormItem>
              )}
            />

            {user?.pending && (
              <FormField
                control={form.control}
                name="approve"
                render={({ field }) => (
                  <FormItem className="grid grid-cols-6 items-center gap-x-4 gap-y-1 space-y-0">
                    <FormLabel className="col-span-2">{t('admin.users.pending')}</FormLabel>
                    <div className="col-span-4 flex items-center gap-3">
                      <FormControl>
                        <Switch checked={field.value} onCheckedChange={field.onChange} />
                      </FormControl>
                      <span className="text-muted-foreground text-sm">{t('admin.users.approve')}</span>
                    </div>
                    <FormDescription className="col-span-4 col-start-3 text-xs">
                      {t('admin.users.pending-hint')}
                    </FormDescription>
                  </FormItem>
                )}
              />
            )}

            {isEdit && (
              <FormField
                control={form.control}
                name="storageGb"
                render={({ field }) => (
                  <FormItem className="grid grid-cols-6 items-center gap-x-4 gap-y-1 space-y-0">
                    <FormLabel className="col-span-2">{t('admin.users.form.storage')}</FormLabel>
                    <div className="col-span-4 flex items-center gap-2">
                      <FormControl>
                        <Input type="number" inputMode="decimal" step="0.5" min={0} className="w-32" placeholder={t('admin.users.form.storage-default')} {...field} />
                      </FormControl>
                      <span className="text-muted-foreground text-sm">GB</span>
                    </div>
                    <FormDescription className="col-span-4 col-start-3 text-xs">
                      {t('admin.users.form.storage-hint', { used: formatSize(user.storageUsedBytes ?? 0) })}
                    </FormDescription>
                    <FormMessage className="col-span-4 col-start-3" />
                  </FormItem>
                )}
              />
            )}

            <FormField
              control={form.control}
              name="password"
              render={({ field }) => (
                <FormItem className="grid grid-cols-6 items-center gap-x-4 gap-y-1 space-y-0">
                  <FormLabel className="col-span-2">{t('admin.users.form.password')}</FormLabel>
                  <FormControl>
                    <PasswordInput
                      className="col-span-4"
                      autoComplete="new-password"
                      placeholder={
                        isEdit ? t('admin.users.form.password-keep') : 'S3cur3P@ssw0rd'
                      }
                      {...field}
                    />
                  </FormControl>
                  <FormMessage className="col-span-4 col-start-3" />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="confirmPassword"
              render={({ field }) => (
                <FormItem className="grid grid-cols-6 items-center gap-x-4 gap-y-1 space-y-0">
                  <FormLabel className="col-span-2">
                    {t('admin.users.form.confirm-password')}
                  </FormLabel>
                  <FormControl>
                    <PasswordInput
                      className="col-span-4"
                      autoComplete="new-password"
                      disabled={!password}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage className="col-span-4 col-start-3" />
                </FormItem>
              )}
            />
          </form>
        </Form>
      </ScrollArea>

      <DialogFooter>
        <Button variant="outline" disabled={pending} onClick={() => onOpenChange(false)}>
          {t('admin.ui.cancel')}
        </Button>
        <Button type="submit" form="users-action-form" disabled={pending}>
          {t('admin.ui.save')}
        </Button>
      </DialogFooter>
    </>
  )
}
