import { useEffect, useState, type ReactNode } from 'react'
import { Modal, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import Animated, { Easing, runOnJS, useAnimatedStyle, useReducedMotion, useSharedValue, withTiming } from 'react-native-reanimated'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { useColors } from '@/theme'

/** How long the sheet takes to come up or go down, in ms. */
const SHEET_MS = 240

/**
 * A panel from the foot of the screen over a dimmed page; a tap outside closes
 * it. The panel slides up and the page dims where it is - the dimming fades in
 * and out in place, never sliding along with the panel. The modal stays until
 * the panel has gone down.
 */
export function Sheet({
  open,
  onClose,
  title,
  floating = true,
  children,
}: {
  open: boolean
  onClose: () => void
  title?: string
  /** A card that stands clear of the screen's edges, rounded all round (the default), rather than one joined to its foot. */
  floating?: boolean
  children: ReactNode
}) {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const reduced = useReducedMotion()
  // Still on screen while it goes down after `open` turned false.
  const [mounted, setMounted] = useState(open)
  const [height, setHeight] = useState(600)
  const shown = useSharedValue(0)
  if (open && !mounted) setMounted(true)

  useEffect(() => {
    if (!mounted) return
    const duration = reduced ? 0 : SHEET_MS
    shown.value = withTiming(open ? 1 : 0, { duration, easing: Easing.out(Easing.cubic) }, (finished) => {
      if (finished && !open) runOnJS(setMounted)(false)
    })
  }, [open, mounted, reduced, shown])

  const dim = useAnimatedStyle(() => ({ opacity: shown.value }))
  // A floating card goes down past its own gap too, so none of it shows at rest.
  const travel = height + (floating ? insets.bottom + 8 : 0)
  const slide = useAnimatedStyle(() => ({ transform: [{ translateY: (1 - shown.value) * travel }] }))

  return (
    <Modal visible={mounted} transparent animationType="none" onRequestClose={onClose} statusBarTranslucent navigationBarTranslucent>
      <Animated.View style={[StyleSheet.absoluteFill, { backgroundColor: colors.overlay }, dim]}>
        <Pressable style={styles.scrim} onPress={onClose} accessibilityLabel="close" />
      </Animated.View>
      <View style={styles.foot} pointerEvents="box-none">
        <Animated.View
          onLayout={(event) => setHeight(event.nativeEvent.layout.height)}
          style={[
            styles.sheet,
            { backgroundColor: colors.card },
            floating ? [styles.floating, { marginBottom: insets.bottom + 8 }] : { paddingBottom: insets.bottom + 12 },
            slide,
          ]}
        >
          <View style={[styles.grip, { backgroundColor: colors.line }]} />
          {title ? <Text style={[styles.title, { color: colors.text }]}>{title}</Text> : null}
          <ScrollView bounces={false}>{children}</ScrollView>
        </Animated.View>
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
  foot: { flex: 1, justifyContent: 'flex-end' },
  sheet: { maxHeight: '75%', borderTopLeftRadius: 24, borderTopRightRadius: 24, paddingHorizontal: 12, paddingTop: 8 },
  floating: { marginHorizontal: 10, borderRadius: 32, paddingHorizontal: 14, paddingBottom: 14 },
  grip: { alignSelf: 'center', width: 36, height: 4, borderRadius: 2, marginBottom: 12 },
  title: { fontSize: 15, fontWeight: '600', paddingHorizontal: 12, marginBottom: 8 },
  option: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingHorizontal: 12, paddingVertical: 12, borderRadius: 14 },
  optionBody: { flex: 1, gap: 2 },
  optionLabel: { fontSize: 15, lineHeight: 21 },
  optionDetail: { fontSize: 13, lineHeight: 18 },
  disabled: { opacity: 0.45 },
})
