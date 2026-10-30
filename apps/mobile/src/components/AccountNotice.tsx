import type { ReactNode } from 'react'
import { ScrollView, StyleSheet, Text, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { useTranslation } from 'react-i18next'

import { categoryKey, type AccountDisabled } from '@openchat/chat-core/account-disabled'
import type { AdminContact } from '@openchat/chat-core/auth'

import { useColors } from '@/theme'
import { Button } from './Button'

interface Action {
  label: string
  onPress: () => void
  busy?: boolean
}

interface Props {
  tone: 'wait' | 'refused'
  title: string
  subtitle: string
  text: string
  /** Between the body and the administrator: why, for a disabled account. */
  children?: ReactNode
  admin?: AdminContact
  /** A line under the body, e.g. the outcome of checking again. */
  status?: string
  action: Action
  secondary?: Action
}

/**
 * An account that exists but cannot be used - waiting for an administrator,
 * or disabled - in place of the form (the web's AccountOverlay).
 */
export function AccountNotice({ tone, title, subtitle, text, children, admin, status, action, secondary }: Props) {
  const { t } = useTranslation()
  const colors = useColors()
  const insets = useSafeAreaInsets()
  return (
    <ScrollView
      style={{ backgroundColor: colors.bg }}
      contentContainerStyle={[styles.content, { paddingTop: insets.top + 72, paddingBottom: insets.bottom + 32 }]}
    >
      <View style={[styles.dot, { backgroundColor: tone === 'wait' ? colors.wait : colors.errInk }]} />
      <Text style={[styles.title, { color: colors.text }]}>{title}</Text>
      <Text style={[styles.title, styles.subtitle, { color: colors.text3 }]}>{subtitle}</Text>
      <Text style={[styles.text, { color: colors.text2 }]}>{text}</Text>
      {children}
      {admin ? (
        <View style={[styles.admin, { backgroundColor: colors.field }]}>
          <Text style={[styles.adminName, { color: colors.text }]}>{t('auth.overlay.admin', { name: admin.name })}</Text>
          <Text style={[styles.adminEmail, { color: colors.text2 }]} selectable>
            {admin.email}
          </Text>
        </View>
      ) : null}
      {status ? <Text style={[styles.status, { color: colors.text2 }]}>{status}</Text> : null}
      <View style={styles.actions}>
        <Button label={action.label} onPress={action.onPress} busy={action.busy} />
        {secondary ? <Button kind="secondary" label={secondary.label} onPress={secondary.onPress} busy={secondary.busy} /> : null}
      </View>
    </ScrollView>
  )
}

/** The title, subtitle and body for a disabled account, by who disabled it. */
export function useDisabledWords(details: AccountDisabled | undefined) {
  const { t } = useTranslation()
  if (!details) return { title: t('auth.banned.title'), subtitle: t('auth.banned.subtitle'), text: t('auth.banned.text') }
  if (details.reason === 'moderation') {
    return {
      title: t('auth.disabled.moderation-title'),
      subtitle: t('auth.disabled.moderation-subtitle'),
      text:
        details.strikes && details.windowHours
          ? t('auth.disabled.moderation-text', { count: details.strikes, hours: details.windowHours })
          : t('auth.disabled.moderation-text-bare'),
    }
  }
  return { title: t('auth.disabled.admin-title'), subtitle: t('auth.disabled.admin-subtitle'), text: t('auth.disabled.admin-text') }
}

/** Why and since when, as the server said. */
export function DisabledReason({ details }: { details: AccountDisabled }) {
  const { t, i18n } = useTranslation()
  const colors = useColors()
  const moderation = details.reason === 'moderation'
  const categories = (details.categories ?? []).map((category) =>
    t(`admin.moderation.category.${categoryKey(category)}` as 'admin.moderation.category.sexual', { defaultValue: category }),
  )
  const reason = moderation
    ? categories.length > 0
      ? t('auth.disabled.moderation-reason-categories', { categories: categories.join(i18n.language === 'zh' ? '、' : ', ') })
      : t('auth.disabled.moderation-reason')
    : details.note
      ? t('auth.disabled.note', { note: details.note })
      : t('auth.disabled.no-note')
  const at = details.disabledAt ? new Date(details.disabledAt) : null
  return (
    <View style={styles.reason}>
      <Text style={[styles.term, { color: colors.text3 }]}>{t('auth.disabled.reason')}</Text>
      <Text style={[styles.definition, { color: colors.text }]}>{reason}</Text>
      {at && !Number.isNaN(at.getTime()) ? (
        <>
          <Text style={[styles.term, { color: colors.text3 }]}>
            {t(moderation ? 'auth.disabled.moderation-at' : 'auth.disabled.admin-at')}
          </Text>
          <Text style={[styles.definition, { color: colors.text }]}>
            {at.toLocaleString(i18n.language, { dateStyle: 'long', timeStyle: 'short' })}
          </Text>
        </>
      ) : null}
    </View>
  )
}

const styles = StyleSheet.create({
  content: { flexGrow: 1, paddingHorizontal: 24 },
  dot: { width: 10, height: 10, borderRadius: 5, marginBottom: 20 },
  title: { fontSize: 28, lineHeight: 34, fontWeight: '500' },
  subtitle: { fontWeight: '400' },
  text: { marginTop: 20, fontSize: 15, lineHeight: 23 },
  reason: { marginTop: 20, gap: 4 },
  term: { fontSize: 13, lineHeight: 18, marginTop: 8 },
  definition: { fontSize: 15, lineHeight: 22 },
  admin: { marginTop: 24, padding: 16, borderRadius: 20, gap: 2 },
  adminName: { fontSize: 15, lineHeight: 22, fontWeight: '500' },
  adminEmail: { fontSize: 14, lineHeight: 20 },
  status: { marginTop: 16, fontSize: 14, lineHeight: 20 },
  actions: { marginTop: 32, gap: 12 },
})
