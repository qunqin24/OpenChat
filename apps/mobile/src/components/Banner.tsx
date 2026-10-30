import { CircleAlert } from 'lucide-react-native'
import { StyleSheet, Text, View } from 'react-native'

import { useColors } from '@/theme'

/** A failure of the whole form rather than of one field. */
export function Banner({ text }: { text: string }) {
  const colors = useColors()
  return (
    <View style={[styles.banner, { backgroundColor: colors.errFill }]} accessibilityRole="alert">
      <CircleAlert size={16} color={colors.errInk} style={styles.icon} />
      <Text style={[styles.text, { color: colors.errInk }]}>{text}</Text>
    </View>
  )
}

const styles = StyleSheet.create({
  banner: { flexDirection: 'row', gap: 8, padding: 12, borderRadius: 16, marginBottom: 16 },
  icon: { marginTop: 2 },
  text: { flex: 1, fontSize: 14, lineHeight: 20 },
})
