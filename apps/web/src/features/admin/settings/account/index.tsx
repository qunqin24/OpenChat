import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { MIN_PASSWORD_LENGTH } from '@openchat/chat-core/auth-validation'
import { isAuthFieldError } from '@/features/auth'
import { useChangePassword } from '@/features/auth/use-auth'
import { PasswordInput } from '@/components/shadcn/password-input'
import { Button } from '@/components/shadcn/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/shadcn/ui/form'
import { ContentSection } from '../components/content-section'

/**
 * Upstream edits a display name, date of birth and language here. The one
 * account setting this app actually has that does not belong on the profile
 * tab is the password, because changing it is an authentication step rather
 * than an edit — it asks for the current one first.
 */
export function SettingsAccount() {
  const { t } = useTranslation()
  const changePassword = useChangePassword()

  const schema = z
    .object({
      currentPassword: z.string().min(1, t('admin.users.form.password-required')),
      newPassword: z.string().min(MIN_PASSWORD_LENGTH, t('admin.users.form.password-short', { n: MIN_PASSWORD_LENGTH })),
      confirmPassword: z.string(),
    })
    .refine(({ newPassword, confirmPassword }) => newPassword === confirmPassword, {
      path: ['confirmPassword'],
      message: t('admin.users.form.password-mismatch'),
    })

  const form = useForm<z.input<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: { currentPassword: '', newPassword: '', confirmPassword: '' },
  })

  const submit = async (values: z.output<typeof schema>) => {
    try {
      await changePassword.mutateAsync({
        currentPassword: values.currentPassword,
        newPassword: values.newPassword,
      })
      form.reset()
      toast.success(t('admin.settings.account.changed'))
    } catch (error) {
      if (isAuthFieldError(error) && error.field === 'currentPassword') {
        form.setError('currentPassword', { message: t(error.key) })
        return
      }
      if (isAuthFieldError(error) && error.field === 'newPassword') {
        form.setError('newPassword', { message: t(error.key) })
        return
      }
      toast.error(t('auth.error.unknown'))
    }
  }

  return (
    <ContentSection
      title={t('admin.settings.account.title')}
      desc={t('admin.settings.account.desc')}
    >
      <Form {...form}>
        <form className="space-y-8" onSubmit={form.handleSubmit((values) => void submit(values))}>
          <FormField
            control={form.control}
            name="currentPassword"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.settings.account.current-password')}</FormLabel>
                <FormControl>
                  <PasswordInput autoComplete="current-password" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="newPassword"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.settings.account.new-password')}</FormLabel>
                <FormControl>
                  <PasswordInput autoComplete="new-password" {...field} />
                </FormControl>
                <FormDescription>{t('admin.settings.account.revoke-hint')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="confirmPassword"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.users.form.confirm-password')}</FormLabel>
                <FormControl>
                  <PasswordInput autoComplete="new-password" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <Button type="submit" disabled={changePassword.isPending}>
            {t('admin.settings.account.change')}
          </Button>
        </form>
      </Form>
    </ContentSection>
  )
}

export default SettingsAccount
