import { router, useNavigation } from 'expo-router'
import type { DrawerNavigationProp } from '@react-navigation/drawer'
import type { ParamListBase } from '@react-navigation/native'
import { ArrowLeft, ChevronRight, Menu } from 'lucide-react-native'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Pressable, StyleSheet, Text, View } from 'react-native'
import { KeyboardAwareScrollView } from 'react-native-keyboard-controller'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { HeaderIcon, PageHeader } from '@/components/HeaderPill'
import { useColors } from '@/theme'

/**
 * A settings page: the header (PageHeader) with the way back (or, on the
 * first page, the drawer), its title, and what it holds, scrolling clear of the keyboard.
 * `foot` stays under the scroll, for a page's one Save.
 */
export function SettingsPage({ title, top, right, foot, children }: { title: string; top?: boolean; right?: ReactNode; foot?: ReactNode; children: ReactNode }) {
  const { t } = useTranslation()
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const navigation = useNavigation<DrawerNavigationProp<ParamListBase>>()
  return (
    <View style={[styles.page, { backgroundColor: colors.bg, paddingTop: insets.top }]}>
      <PageHeader
        title={title}
        left={
          top ? (
            <HeaderIcon onPress={() => navigation.openDrawer()} label={t('chat.sidebar.open')}>
              <Menu size={20} color={colors.text} />
            </HeaderIcon>
          ) : (
            <HeaderIcon onPress={() => router.back()} label={t('subagents.panel.back')}>
              <ArrowLeft size={20} color={colors.text} />
            </HeaderIcon>
          )
        }
        right={right}
      />
      <KeyboardAwareScrollView
        bottomOffset={24}
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={[styles.body, { paddingBottom: (foot ? 16 : insets.bottom) + 24 }]}
      >
        {children}
      </KeyboardAwareScrollView>
      {foot ? <View style={[styles.foot, { paddingBottom: insets.bottom + 12 }]}>{foot}</View> : null}
    </View>
  )
}

/** Rows that belong together, on a card a step off the page, with a quiet heading. */
export function Group({ title, note, children }: { title?: string; note?: string; children: ReactNode }) {
  const colors = useColors()
  return (
    <View style={styles.group}>
      {title ? <Text style={[styles.groupTitle, { color: colors.text3 }]}>{title}</Text> : null}
      <View style={[styles.card, { backgroundColor: colors.bubble }]}>{children}</View>
      {note ? <Text style={[styles.note, { color: colors.text3 }]}>{note}</Text> : null}
    </View>
  )
}

/** One line of a group: what it is, what it is set to, and where tapping it goes. */
export function Row({
  icon,
  label,
  value,
  onPress,
  danger,
  disabled,
  right,
  chevron = Boolean(onPress),
}: {
  icon?: ReactNode
  label: string
  value?: string
  onPress?: () => void
  danger?: boolean
  disabled?: boolean
  right?: ReactNode
  chevron?: boolean
}) {
  const colors = useColors()
  return (
    <Pressable
      disabled={!onPress || disabled}
      onPress={onPress}
      accessibilityRole={onPress ? 'button' : undefined}
      style={({ pressed }) => [styles.row, pressed && { backgroundColor: colors.field }, disabled && styles.disabled]}
    >
      {icon}
      <Text style={[styles.rowLabel, { color: danger ? colors.errInk : colors.text }]} numberOfLines={1}>
        {label}
      </Text>
      {value ? (
        <Text style={[styles.rowValue, { color: colors.text3 }]} numberOfLines={1}>
          {value}
        </Text>
      ) : null}
      {right}
      {chevron ? <ChevronRight size={17} color={colors.text3} /> : null}
    </Pressable>
  )
}

/** A page with nothing on it yet: a mark, what will be here, and the way to add the first. */
export function EmptyState({ icon, text, action }: { icon: ReactNode; text: string; action?: ReactNode }) {
  const colors = useColors()
  return (
    <View style={styles.empty}>
      <View style={[styles.emptyIcon, { backgroundColor: colors.bubble }]}>{icon}</View>
      <Text style={[styles.emptyText, { color: colors.text2 }]}>{text}</Text>
      {action}
    </View>
  )
}

/** Prose under a heading: what a section is for. */
export function Help({ children, tone }: { children: ReactNode; tone?: 'error' | 'warning' }) {
  const colors = useColors()
  const color = tone === 'error' ? colors.errInk : tone === 'warning' ? colors.warning : colors.text3
  return <Text style={[styles.help, { color }]}>{children}</Text>
}

const styles = StyleSheet.create({
  page: { flex: 1 },
  body: { paddingHorizontal: 16, paddingTop: 8, gap: 20 },
  foot: { paddingHorizontal: 16, paddingTop: 8 },
  group: { gap: 6 },
  groupTitle: { fontSize: 13, paddingHorizontal: 14 },
  card: { borderRadius: 18, overflow: 'hidden' },
  note: { fontSize: 12, lineHeight: 17, paddingHorizontal: 14 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 52, paddingHorizontal: 16, paddingVertical: 12 },
  rowLabel: { flex: 1, fontSize: 15 },
  rowValue: { fontSize: 14, maxWidth: '55%' },
  disabled: { opacity: 0.5 },
  empty: { alignItems: 'center', gap: 14, paddingTop: 48, paddingHorizontal: 24 },
  emptyIcon: { width: 64, height: 64, borderRadius: 32, alignItems: 'center', justifyContent: 'center' },
  emptyText: { fontSize: 14, lineHeight: 21, textAlign: 'center', marginBottom: 6 },
  help: { fontSize: 13, lineHeight: 19, paddingHorizontal: 4 },
})
