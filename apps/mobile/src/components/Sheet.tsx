import type { ReactNode } from 'react'
import { Modal, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { useColors } from '@/theme'

/** A panel from the foot of the screen over a dimmed page; a tap outside closes it. */
export function Sheet({ open, onClose, title, children }: { open: boolean; onClose: () => void; title?: string; children: ReactNode }) {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  return (
    <Modal visible={open} transparent animationType="slide" onRequestClose={onClose} statusBarTranslucent navigationBarTranslucent>
      <Pressable style={[styles.scrim, { backgroundColor: colors.overlay }]} onPress={onClose} accessibilityLabel="close" />
      <View style={[styles.sheet, { backgroundColor: colors.card, paddingBottom: insets.bottom + 12 }]}>
        <View style={[styles.grip, { backgroundColor: colors.line }]} />
        {title ? <Text style={[styles.title, { color: colors.text }]}>{title}</Text> : null}
        <ScrollView bounces={false}>{children}</ScrollView>
      </View>
    </Modal>
  )
}

/** One choice in a sheet: a label, a quieter line under it, a mark when chosen. */
export function SheetOption({
  label,
  detail,
  selected,
  disabled,
  onPress,
  right,
}: {
  label: string
  detail?: string
  selected?: boolean
  disabled?: boolean
  onPress: () => void
  right?: ReactNode
}) {
  const colors = useColors()
  return (
    <Pressable
      disabled={disabled}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityState={{ selected, disabled }}
      style={({ pressed }) => [styles.option, { backgroundColor: selected || pressed ? colors.field : 'transparent' }, disabled && styles.disabled]}
    >
      <View style={styles.optionBody}>
        <Text style={[styles.optionLabel, { color: colors.text }]}>{label}</Text>
        {detail ? <Text style={[styles.optionDetail, { color: colors.text3 }]}>{detail}</Text> : null}
      </View>
      {right}
    </Pressable>
  )
}

const styles = StyleSheet.create({
  scrim: { flex: 1 },
  sheet: { maxHeight: '75%', borderTopLeftRadius: 24, borderTopRightRadius: 24, paddingHorizontal: 12, paddingTop: 8 },
  grip: { alignSelf: 'center', width: 36, height: 4, borderRadius: 2, marginBottom: 12 },
  title: { fontSize: 15, fontWeight: '600', paddingHorizontal: 12, marginBottom: 8 },
  option: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingHorizontal: 12, paddingVertical: 12, borderRadius: 14 },
  optionBody: { flex: 1, gap: 2 },
  optionLabel: { fontSize: 15, lineHeight: 21 },
  optionDetail: { fontSize: 13, lineHeight: 18 },
  disabled: { opacity: 0.45 },
})
