import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { MIN_PASSWORD_LENGTH, validateEmail, validateName } from '@/components/auth/validation'
import { UserAvatar } from '@/components/user-avatar'
import { isAuthFieldError, type AuthErrorKey } from '@/features/auth'
import { useChangePassword, useSession, useUpdateProfile } from '@/features/auth/use-auth'
import { Field } from './Field'

/**
 * Name, email and password, through the self-service endpoints (PATCH /auth/me,
 * POST /auth/password) — the ones that cannot reach the role. As in Open WebUI
 * the account is a block of avatar and fields with one Save at the foot, and
 * the password is a section that folds away. Validation is written by hand, as
 * on the sign-in pages. There are no pictures to upload: the avatar is the
 * first letter of the name.
 */
export function AccountTab() {
  const { data: session } = useSession()
  if (!session) return null
  return <Profile key={session.id} id={session.id} name={session.name} email={session.email} />
}

function Profile({ id, name, email }: { id: string; name: string; email: string }) {
  const { t } = useTranslation()
  const update = useUpdateProfile()
  const [draft, setDraft] = useState({ name, email })
  const [errors, setErrors] = useState<{ name?: AuthErrorKey; email?: AuthErrorKey }>({})
  const dirty = draft.name.trim() !== name || draft.email.trim() !== email

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    const found = {
      name: validateName(draft.name) ?? undefined,
      email: validateEmail(draft.email) ?? undefined,
    }
    setErrors(found)
    if (found.name || found.email) return
    try {
      await update.mutateAsync({
        ...(draft.name.trim() !== name ? { name: draft.name.trim() } : {}),
        ...(draft.email.trim() !== email ? { email: draft.email.trim() } : {}),
      })
      toast.success(t('settings.account.saved'))
    } catch (error) {
      if (isAuthFieldError(error) && (error.field === 'name' || error.field === 'email')) {
        setErrors({ [error.field]: error.key })
        return
      }
      toast.error(t('auth.error.unknown'))
    }
  }

  return (
    <form className="n-settings-tab-body" id="tab-account" onSubmit={(e) => void submit(e)} noValidate>
      <div className="n-settings-scroll">
        <div className="n-settings-heading-block">
          <div className="n-settings-heading">{t('settings.account.title')}</div>
          <p className="n-settings-help">{t('settings.account.desc')}</p>
        </div>

        <div className="n-settings-profile">
          <UserAvatar id={id} name={name} email={email} className="size-16 text-2xl" />
          <div className="n-settings-profile-fields">
            <Field
              label={t('settings.account.name')}
              hint={t('settings.account.name-hint')}
              value={draft.name}
              error={errors.name && t(errors.name)}
              autoComplete="username"
              onChange={(value) => setDraft((d) => ({ ...d, name: value }))}
            />
            <Field
              label={t('settings.account.email')}
              type="email"
              value={draft.email}
              error={errors.email && t(errors.email)}
              autoComplete="email"
              onChange={(value) => setDraft((d) => ({ ...d, email: value }))}
            />
          </div>
        </div>

        <hr className="n-settings-rule" />
        <Password />
      </div>

      <div className="n-settings-foot">
        <button type="submit" className="n-settings-save" disabled={!dirty || update.isPending}>
          {t('settings.save')}
        </button>
      </div>
    </form>
  )
}

/** The password section: folded away until asked for, with a button of its own. */
function Password() {
  const { t } = useTranslation()
  const change = useChangePassword()
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState({ current: '', next: '', confirm: '' })
  const [errors, setErrors] = useState<{ current?: string; next?: string; confirm?: string }>({})

  const submit = async () => {
    const found = {
      current: draft.current ? undefined : t('auth.error.password-required'),
      next:
        draft.next.length < MIN_PASSWORD_LENGTH
          ? t(draft.next ? 'auth.error.password-short' : 'auth.error.password-required')
          : undefined,
      confirm: draft.confirm === draft.next ? undefined : t('settings.account.password-mismatch'),
    }
    setErrors(found)
    if (found.current || found.next || found.confirm) return
    try {
      await change.mutateAsync({ currentPassword: draft.current, newPassword: draft.next })
      setDraft({ current: '', next: '', confirm: '' })
      setOpen(false)
      toast.success(t('settings.account.password-changed'))
    } catch (error) {
      if (isAuthFieldError(error) && error.field === 'currentPassword') {
        setErrors({ current: t(error.key) })
        return
      }
      if (isAuthFieldError(error) && error.field === 'newPassword') {
        setErrors({ next: t(error.key) })
        return
      }
      toast.error(t('auth.error.unknown'))
    }
  }

  return (
    <div className="n-settings-password">
      <div className="n-settings-line">
        <div className="n-settings-subheading">{t('settings.account.password')}</div>
        <button
          type="button"
          className="n-settings-toggle"
          aria-expanded={open}
          aria-controls="settings-password"
          onClick={() => setOpen(!open)}
        >
          {t(open ? 'settings.account.hide' : 'settings.account.show')}
        </button>
      </div>
      {open && (
        // Not a <form>: it sits inside the profile's, and forms do not nest.
        <div id="settings-password" className="n-settings-password-fields">
          <Field
            label={t('settings.account.current-password')}
            placeholder={t('auth.field.password-placeholder')}
            type="password"
            value={draft.current}
            error={errors.current}
            autoComplete="current-password"
            onChange={(value) => setDraft((d) => ({ ...d, current: value }))}
          />
          <Field
            label={t('settings.account.new-password')}
            hint={t('settings.account.password-hint')}
            placeholder={t('settings.account.new-placeholder')}
            type="password"
            value={draft.next}
            error={errors.next}
            autoComplete="new-password"
            onChange={(value) => setDraft((d) => ({ ...d, next: value }))}
          />
          <Field
            label={t('settings.account.confirm-password')}
            placeholder={t('settings.account.confirm-placeholder')}
            type="password"
            value={draft.confirm}
            error={errors.confirm}
            autoComplete="new-password"
            onChange={(value) => setDraft((d) => ({ ...d, confirm: value }))}
          />
          <button
            type="button"
            className="n-settings-soft"
            disabled={change.isPending || !draft.current || !draft.next}
            onClick={() => void submit()}
          >
            {t('settings.account.change-password')}
          </button>
        </div>
      )}
    </div>
  )
}

export default AccountTab
