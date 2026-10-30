import { Pressable, StyleSheet, Text, View } from 'react-native'

import { useColors } from '@/theme'

/** A line across the transcript with a word in it: where the context was compressed, a pause in the talk. */
export function Divider({ label, action }: { label: string; action?: { label: string; onPress: () => void; busy?: boolean } }) {
  const colors = useColors()
  return (
    <View style={styles.row}>
      <View style={[styles.line, { backgroundColor: colors.line }]} />
      <Text style={[styles.label, { color: colors.text3 }]}>{label}</Text>
      {action ? (
        <Pressable hitSlop={8} disabled={action.busy} onPress={action.onPress} accessibilityRole="button">
          <Text style={[styles.label, styles.action, { color: colors.text2 }, action.busy && styles.busy]}>{action.label}</Text>
        </Pressable>
      ) : null}
      <View style={[styles.line, { backgroundColor: colors.line }]} />
    </View>
  )
}

const styles = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  line: { flex: 1, height: StyleSheet.hairlineWidth },
  label: { fontSize: 12 },
  action: { textDecorationLine: 'underline' },
  busy: { opacity: 0.5 },
})
