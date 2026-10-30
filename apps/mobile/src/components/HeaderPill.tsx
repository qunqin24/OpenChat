import type { ReactNode } from 'react'
import { Pressable, StyleSheet, Text, View, type StyleProp, type ViewStyle } from 'react-native'

import { useColors } from '@/theme'

/** How tall the header's pills are, and so how wide a one-icon one is. */
export const PILL = 44

/**
 * A header's buttons as raised pills: white, edged and lifted off the page like
 * the composer, so the header reads as a few things to tap floating over the
 * chat rather than a bar. One icon makes a circle; several share one pill.
 */
export function HeaderPill({ children, style }: { children: ReactNode; style?: StyleProp<ViewStyle> }) {
  const colors = useColors()
  return <View style={[styles.pill, { backgroundColor: colors.card, borderColor: colors.line }, style]}>{children}</View>
}

/** One icon in a header pill. */
export function HeaderIcon({ onPress, label, disabled, children }: { onPress: () => void; label: string; disabled?: boolean; children: ReactNode }) {
  const colors = useColors()
  return (
    <Pressable
      onPress={onPress}
      hitSlop={4}
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled }}
      disabled={disabled}
      style={({ pressed }) => [styles.icon, pressed && { backgroundColor: colors.field }, disabled && styles.dim]}
    >
      {children}
    </Pressable>
  )
}

/**
 * A page's header in the same pills as the chat's: the way back (or to the
 * drawer) on the left, the title in the middle, and at most one action on the
 * right.
 */
export function PageHeader({ title, left, right }: { title: string; left: ReactNode; right?: ReactNode }) {
  const colors = useColors()
  return (
    <View style={styles.header}>
      <HeaderPill>{left}</HeaderPill>
      <Text style={[styles.title, { color: colors.text }]} numberOfLines={1}>
        {title}
      </Text>
      {right ? <HeaderPill>{right}</HeaderPill> : <View style={styles.spacer} />}
    </View>
  )
}

const styles = StyleSheet.create({
  header: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingHorizontal: 14, paddingTop: 4, paddingBottom: 10 },
  title: { flex: 1, fontSize: 17, fontWeight: '600', textAlign: 'center' },
  spacer: { width: PILL },
  dim: { opacity: 0.35 },
  pill: {
    flexDirection: 'row',
    alignItems: 'center',
    height: PILL,
    minWidth: PILL,
    borderRadius: PILL / 2,
    borderWidth: StyleSheet.hairlineWidth,
    boxShadow: '0 4px 14px rgba(0, 0, 0, 0.06)',
  },
  icon: { width: PILL, height: PILL - 2, borderRadius: PILL / 2, alignItems: 'center', justifyContent: 'center' },
})
