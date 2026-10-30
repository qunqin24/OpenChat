import { StyleSheet, Text, View } from 'react-native'
import Animated, { FadeInDown, FadeOutDown } from 'react-native-reanimated'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { useToasts } from '@/lib/toast'
import { useColors } from '@/theme'

/** The toasts (lib/toast.ts), above the composer at the foot of the screen. */
export function Toaster() {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const toasts = useToasts((s) => s.toasts)
  return (
    <View pointerEvents="box-none" style={[StyleSheet.absoluteFill, styles.layer, { paddingBottom: insets.bottom + 120 }]}>
      {toasts.map((toast) => (
        <Animated.View
          key={toast.id}
          entering={FadeInDown.duration(180)}
          exiting={FadeOutDown.duration(180)}
          style={[styles.toast, { backgroundColor: colors.accent }]}
          accessibilityLiveRegion="polite"
        >
          <Text style={[styles.text, { color: colors.accentFg }]}>{toast.text}</Text>
          {toast.detail ? <Text style={[styles.detail, { color: colors.accentFg }]}>{toast.detail}</Text> : null}
        </Animated.View>
      ))}
    </View>
  )
}

const styles = StyleSheet.create({
  layer: { justifyContent: 'flex-end', alignItems: 'center', paddingHorizontal: 20 },
  toast: { borderRadius: 16, paddingHorizontal: 16, paddingVertical: 12, maxWidth: 480, width: '100%', gap: 2 },
  text: { fontSize: 14, lineHeight: 20 },
  detail: { fontSize: 12, lineHeight: 17, opacity: 0.75 },
})
