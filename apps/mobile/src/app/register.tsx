import { Link, useRouter } from 'expo-router'
import { Lock } from 'lucide-react-native'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { StyleSheet, Text, View } from 'react-native'

import {
  MIN_PASSWORD_LENGTH,
  validateEmail,
  validateName,
  validatePassword,
} from '@openchat/chat-core/auth-validation'
import { isAuthFieldError, type AdminContact, type AuthErrorKey } from '@openchat/chat-core/auth'

import { AccountNotice } from '@/components/AccountNotice'
import { AuthField, type FieldInput } from '@/components/AuthField'
import { AuthShell } from '@/components/AuthShell'
import { Banner } from '@/components/Banner'
import { Button } from '@/components/Button'
import { failureKey, useAuthOptions, useRegister } from '@/lib/session'
import { useColors } from '@/theme'

type Field = 'name' | 'email' | 'password'
type Errors = Partial<Record<Field, AuthErrorKey>>

/**
 * Creating an account. One that works at once is signed in (the guard takes
 * it into the app); one that waits for an administrator says so here.
 */
export default function RegisterScreen() {
  const { t } = useTranslation()
  const colors = useColors()
  const router = useRouter()
  const options = useAuthOptions()
  const register = useRegister()
  const email$ = useRef<FieldInput>(null)
  const password$ = useRef<FieldInput>(null)

  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [errors, setErrors] = useState<Errors>({})
  const [waiting, setWaiting] = useState<{ admin?: AdminContact } | null>(null)

  const submit = () => {
    if (register.isPending) return
    const next: Errors = {}
    const nameError = validateName(name)
    const emailError = validateEmail(email)
    const passwordError = validatePassword(password, { signup: true })
    if (nameError) next.name = nameError
    if (emailError) next.email = emailError
    if (passwordError) next.password = passwordError
    setErrors(next)
    if (Object.keys(next).length > 0) return
    register.mutate(
      { name: name.trim(), email: email.trim(), password },
      { onSuccess: (outcome) => outcome.status === 'pending' && setWaiting({ admin: outcome.admin }) },
    )
  }

  const toLogin = () => router.replace('/login')

  if (waiting) {
    return (
      <AccountNotice
        tone="wait"
        title={t('auth.pending.title')}
        subtitle={t('auth.pending.subtitle')}
        text={t('auth.pending.text')}
        admin={waiting.admin}
        action={{ label: t('auth.pending.back'), onPress: toLogin }}
      />
    )
  }

  if (options.data && !options.data.registration) {
    return (
      <AuthShell title={t('auth.register.closed-title')}>
        <View style={[styles.lock, { backgroundColor: colors.field }]}>
          <Lock size={20} color={colors.text2} />
        </View>
        <Text style={[styles.text, { color: colors.text2 }]}>{t('auth.register.closed')}</Text>
        <Button label={t('auth.register.to-login')} onPress={toLogin} />
      </AuthShell>
    )
  }

  const failure = register.error
  const formError = failure && (!isAuthFieldError(failure) || failure.field === 'form') ? failureKey(failure) : null
  const fieldError = (field: Field) => {
    if (errors[field]) return t(errors[field])
    if (isAuthFieldError(failure) && failure.field === field) return t(failure.key)
    return undefined
  }
  const edited = (field: Field) => setErrors((e) => ({ ...e, [field]: undefined }))

  return (
    <AuthShell title={t('auth.register.title')} subtitle={t('auth.register.subtitle')}>
      {formError ? <Banner text={t(formError)} /> : null}
      <AuthField
        label={t('auth.field.name')}
        placeholder={t('auth.field.name-placeholder')}
        value={name}
        onChangeText={(value) => {
          setName(value)
          edited('name')
        }}
        error={fieldError('name')}
        autoComplete="username-new"
        textContentType="username"
        returnKeyType="next"
        submitBehavior="submit"
        onSubmitEditing={() => email$.current?.focus()}
        editable={!register.isPending}
      />
      <AuthField
        ref={email$}
        label={t('auth.field.email')}
        placeholder={t('auth.field.email-placeholder')}
        value={email}
        onChangeText={(value) => {
          setEmail(value)
          edited('email')
        }}
        error={fieldError('email')}
        autoComplete="email"
        textContentType="emailAddress"
        keyboardType="email-address"
        returnKeyType="next"
        submitBehavior="submit"
        onSubmitEditing={() => password$.current?.focus()}
        editable={!register.isPending}
      />
      <AuthField
        ref={password$}
        label={t('auth.field.password')}
        hint={t('auth.register.password-hint', { min: MIN_PASSWORD_LENGTH })}
        placeholder={t('auth.field.password-placeholder')}
        value={password}
        onChangeText={(value) => {
          setPassword(value)
          edited('password')
        }}
        error={fieldError('password')}
        secret
        autoComplete="new-password"
        textContentType="newPassword"
        returnKeyType="go"
        onSubmitEditing={submit}
        editable={!register.isPending}
      />
      <View style={styles.actions}>
        <Button label={t('auth.register.action')} onPress={submit} busy={register.isPending} />
      </View>
      <View style={styles.footer}>
        <Text style={[styles.footerText, { color: colors.text2 }]}>{t('auth.register.have-account')} </Text>
        <Link href="/login" replace>
          <Text style={[styles.footerText, styles.link, { color: colors.text }]}>{t('auth.login.action')}</Text>
        </Link>
      </View>
    </AuthShell>
  )
}

const styles = StyleSheet.create({
  lock: { width: 48, height: 48, borderRadius: 24, alignItems: 'center', justifyContent: 'center', marginBottom: 20 },
  text: { fontSize: 15, lineHeight: 23, marginBottom: 28 },
  actions: { marginTop: 8 },
  footer: { flexDirection: 'row', justifyContent: 'center', marginTop: 28 },
  footerText: { fontSize: 14, lineHeight: 20 },
  link: { fontWeight: '500' },
})
