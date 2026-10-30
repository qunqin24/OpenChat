import type { ReactNode } from 'react'
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native'

import { useColors } from '@/theme'

interface Props {
  label: string
  onPress: () => void
  /** The black pill, or the quieter filled one beside it. */
  kind?: 'primary' | 'secondary'
  busy?: boolean
  disabled?: boolean
  icon?: ReactNode
}

export function Button({ label, onPress, kind = 'primary', busy, disabled, icon }: Props) {
  const colors = useColors()
  const primary = kind === 'primary'
  const ink = primary ? colors.accentFg : colors.text
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ disabled: disabled || busy, busy }}
      disabled={disabled || busy}
      onPress={onPress}
      style={({ pressed }) => [
        styles.button,
        { backgroundColor: primary ? colors.accent : colors.field },
        (disabled || busy) && styles.dimmed,
        pressed && styles.pressed,
      ]}
    >
      <View style={styles.row}>
        {busy ? <ActivityIndicator size="small" color={ink} /> : icon}
        <Text style={[styles.label, { color: ink }]}>{label}</Text>
      </View>
    </Pressable>
  )
}

const styles = StyleSheet.create({
  button: { height: 48, borderRadius: 999, justifyContent: 'center', paddingHorizontal: 16 },
  row: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8 },
  label: { fontSize: 15, lineHeight: 20, fontWeight: '500' },
  dimmed: { opacity: 0.7 },
  pressed: { transform: [{ scale: 0.985 }] },
})
