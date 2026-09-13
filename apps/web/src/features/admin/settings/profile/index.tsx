import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { isAuthFieldError } from '@/features/auth'
import { useSession, useUpdateProfile } from '@/features/auth/use-auth'
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
import { Input } from '@/components/shadcn/ui/input'
import { ContentSection } from '../components/content-section'

/**
 * Upstream's profile form is username, bio and a repeatable list of URLs, none
 * of which the account model has. What it does have is the name and address
 * every other screen identifies you by, which is what this edits.
 */
export function SettingsProfile() {
  const { t } = useTranslation()
  const { data: session } = useSession()
  const updateProfile = useUpdateProfile()

  const schema = z.object({
    name: z
      .string()
      .trim()
      .min(1, t('admin.users.form.name-required'))
      .max(80)
      // Sign-in takes a username or an address in one field; a CHECK
      // constraint keeps the two name spaces disjoint.
      .refine((value) => !value.includes('@'), t('admin.users.form.name-invalid')),
    email: z.email(t('admin.users.form.email-invalid')),
  })

  const form = useForm<z.input<typeof schema>>({
    resolver: zodResolver(schema),
    values: { name: session?.name ?? '', email: session?.email ?? '' },
  })

  const submit = async (values: z.output<typeof schema>) => {
    // Sending an unchanged field would be a no-op the server answers with
    // "nothing to update" when neither moved.
    const changed = {
      ...(values.name !== session?.name ? { name: values.name } : {}),
      ...(values.email !== session?.email ? { email: values.email } : {}),
    }
    if (Object.keys(changed).length === 0) return

    try {
      await updateProfile.mutateAsync(changed)
      toast.success(t('admin.settings.saved'))
    } catch (error) {
      if (isAuthFieldError(error) && (error.field === 'email' || error.field === 'name')) {
        form.setError(error.field, { message: t(error.key) })
        return
      }
      toast.error(t('auth.error.unknown'))
    }
  }

  return (
    <ContentSection
      title={t('admin.settings.profile.title')}
      desc={t('admin.settings.profile.desc')}
    >
      <Form {...form}>
        <form className="space-y-8" onSubmit={form.handleSubmit((values) => void submit(values))}>
          <FormField
            control={form.control}
            name="name"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.users.form.name')}</FormLabel>
                <FormControl>
                  <Input autoComplete="name" {...field} />
                </FormControl>
                <FormDescription>{t('admin.settings.profile.name-hint')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="email"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('admin.users.form.email')}</FormLabel>
                <FormControl>
                  <Input autoComplete="email" {...field} />
                </FormControl>
                <FormDescription>{t('admin.settings.profile.email-hint')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <Button type="submit" disabled={updateProfile.isPending}>
            {t('admin.settings.save')}
          </Button>
        </form>
      </Form>
    </ContentSection>
  )
}

export default SettingsProfile
