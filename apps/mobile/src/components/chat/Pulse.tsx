import { useEffect } from 'react'
import { StyleSheet } from 'react-native'
import Animated, { useAnimatedStyle, useReducedMotion, useSharedValue, withRepeat, withTiming } from 'react-native-reanimated'

import { useColors } from '@/theme'

/**
 * The model at work: a dot that breathes. An essential indicator, so it moves
 * with reduced motion too, only more gently.
 */
export function Pulse({ size = 8 }: { size?: number }) {
  const colors = useColors()
  const reduced = useReducedMotion()
  const scale = useSharedValue(1)
  useEffect(() => {
    scale.value = withRepeat(withTiming(reduced ? 1.15 : 1.5, { duration: 700 }), -1, true)
  }, [reduced, scale])
  const style = useAnimatedStyle(() => ({ transform: [{ scale: scale.value }], opacity: 1.6 - scale.value * 0.6 }))
  return <Animated.View style={[styles.dot, { width: size, height: size, borderRadius: size / 2, backgroundColor: colors.text }, style]} />
}

const styles = StyleSheet.create({ dot: { marginHorizontal: 2 } })
