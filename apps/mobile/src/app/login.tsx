import { Link } from 'expo-router'
import { KeyRound } from 'lucide-react-native'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { StyleSheet, Text, View } from 'react-native'
import { Passkey } from 'react-native-passkey'

import { validateIdentifier, validatePassword } from '@openchat/chat-core/auth-validation'
import { isAuthFieldError, type AuthErrorKey } from '@openchat/chat-core/auth'

import { AccountNotice, DisabledReason, useDisabledWords } from '@/components/AccountNotice'
import { AuthField, type FieldInput } from '@/components/AuthField'
import { AuthShell } from '@/components/AuthShell'
import { Banner } from '@/components/Banner'
import { Button } from '@/components/Button'
import { failureKey, useAuthOptions, useLogin, usePasskeyLogin } from '@/lib/session'
import { useColors } from '@/theme'

type Errors = Partial<Record<'identifier' | 'password', AuthErrorKey>>

/** Signing in. Getting in is the root layout's: the session changes and the guard follows. */
export default function LoginScreen() {
  const { t } = useTranslation()
  const colors = useColors()
  const options = useAuthOptions()
  const login = useLogin()
  const passkey = usePasskeyLogin()
  const password$ = useRef<FieldInput>(null)

  const [identifier, setIdentifier] = useState('')
  const [password, setPassword] = useState('')
  const [errors, setErrors] = useState<Errors>({})
  // The failure the person has since dismissed; the mutation itself is never
  // reset (resetting mid-flight would reopen the submit path).
  const [dismissed, setDismissed] = useState<unknown>(null)
  // Whether "check again" has been tried on the pending screen.
  const [checked, setChecked] = useState(false)
  const busy = login.isPending || passkey.isPending

  const submit = () => {
    if (busy) return
    const next: Errors = {}
    const identifierError = validateIdentifier(identifier)
    const passwordError = validatePassword(password)
    if (identifierError) next.identifier = identifierError
    if (passwordError) next.password = passwordError
    setErrors(next)
    if (Object.keys(next).length === 0) login.mutate({ identifier, password })
  }

  const signInWithPasskey = () => {
    if (busy) return
    setDismissed(login.error)
    passkey.mutate()
  }

  const failure = login.error && login.error !== dismissed ? login.error : null
  const passkeyFailure = passkey.error && passkey.error !== dismissed ? passkey.error : null
  const refused = isAuthFieldError(failure) ? failure : null
  const disabledWords = useDisabledWords(refused?.disabled)

  // An account that exists but cannot sign in gets the screen, not a banner.
  if (refused?.key === 'auth.error.pending-approval') {
    return (
      <AccountNotice
        tone="wait"
        title={t('auth.pending.title')}
        subtitle={t('auth.pending.subtitle')}
        text={t('auth.pending.text')}
        admin={refused.admin}
        status={checked && !login.isPending ? t('auth.pending.still') : undefined}
        action={{
          label: t('auth.pending.check'),
          // Signs in again with what was just sent: the way in once approved.
          onPress: () => {
            setChecked(true)
            if (login.variables) login.mutate(login.variables)
          },
          busy: login.isPending,
        }}
        secondary={{
          label: t('auth.pending.back'),
          onPress: () => {
            setChecked(false)
            setDismissed(login.error)
          },
        }}
      />
    )
  }
  if (refused?.key === 'auth.error.account-disabled') {
    return (
      <AccountNotice
        tone="refused"
        {...disabledWords}
        admin={refused.admin}
        action={{
          label: t('auth.banned.back'),
          onPress: () => {
            setDismissed(login.error)
            setPassword('')
          },
        }}
      >
        {refused.disabled ? <DisabledReason details={refused.disabled} /> : null}
      </AccountNotice>
    )
  }

  const formError = failure && (!isAuthFieldError(failure) || failure.field === 'form') ? failureKey(failure) : null
  const fieldError = (field: 'identifier' | 'password') => {
    if (errors[field]) return t(errors[field])
    if (isAuthFieldError(failure) && failure.field === field) return t(failure.key)
    return undefined
  }
  const offerPasskey = options.data?.passkeys && Passkey.isSupported()

  return (
    <AuthShell title={t('auth.login.title', { name: t('app.name') })} subtitle={t('auth.login.subtitle')}>
      {formError ? <Banner text={t(formError)} /> : null}
      {passkeyFailure ? <Banner text={t(failureKey(passkeyFailure))} /> : null}
      <AuthField
        label={t('auth.field.identifier')}
        placeholder={t('auth.field.identifier-placeholder')}
        value={identifier}
        onChangeText={(value) => {
          setIdentifier(value)
          setErrors((e) => ({ ...e, identifier: undefined }))
          setDismissed(login.error)
        }}
        error={fieldError('identifier')}
        autoComplete="username"
        textContentType="username"
        keyboardType="email-address"
        returnKeyType="next"
        submitBehavior="submit"
        onSubmitEditing={() => password$.current?.focus()}
        editable={!busy}
      />
      <AuthField
        ref={password$}
        label={t('auth.field.password')}
        placeholder={t('auth.field.password-placeholder')}
        value={password}
        onChangeText={(value) => {
          setPassword(value)
          setErrors((e) => ({ ...e, password: undefined }))
          setDismissed(login.error)
        }}
        error={fieldError('password')}
        secret
        autoComplete="current-password"
        textContentType="password"
        returnKeyType="go"
        onSubmitEditing={submit}
        editable={!busy}
      />
      <View style={styles.actions}>
        <Button label={t('auth.login.action')} onPress={submit} busy={login.isPending} disabled={busy} />
        {offerPasskey ? (
          <Button
            kind="secondary"
            label={t('auth.login.passkey')}
            onPress={signInWithPasskey}
            busy={passkey.isPending}
            disabled={busy}
            icon={<KeyRound size={18} color={colors.text} />}
          />
        ) : null}
      </View>
      {options.data?.registration ? (
        <View style={styles.footer}>
          <Text style={[styles.footerText, { color: colors.text2 }]}>{t('auth.login.no-account')} </Text>
          <Link href="/register" replace>
            <Text style={[styles.footerText, styles.link, { color: colors.text }]}>{t('auth.register.link')}</Text>
          </Link>
        </View>
      ) : null}
    </AuthShell>
  )
}

const styles = StyleSheet.create({
  actions: { marginTop: 8, gap: 12 },
  footer: { flexDirection: 'row', justifyContent: 'center', marginTop: 28 },
  footerText: { fontSize: 14, lineHeight: 20 },
  link: { fontWeight: '500' },
})
