import { StyleSheet, Text, View } from 'react-native'

import { useColors } from '@/theme'

/** A line across the transcript with a word in it: where the context was compressed, a pause in the talk. */
export function Divider({ label }: { label: string }) {
  const colors = useColors()
  return (
    <View style={styles.row}>
      <View style={[styles.line, { backgroundColor: colors.line }]} />
      <Text style={[styles.label, { color: colors.text3 }]}>{label}</Text>
      <View style={[styles.line, { backgroundColor: colors.line }]} />
    </View>
  )
}

const styles = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  line: { flex: 1, height: StyleSheet.hairlineWidth },
  label: { fontSize: 12 },
})
