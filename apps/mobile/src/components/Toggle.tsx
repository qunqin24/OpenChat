import * as Haptics from 'expo-haptics'
import { useEffect } from 'react'
import { Pressable, StyleSheet, useColorScheme } from 'react-native'
import Animated, { interpolateColor, useAnimatedStyle, useReducedMotion, useSharedValue, withTiming } from 'react-native-reanimated'

import { useColors } from '@/theme'

const WIDTH = 50
const HEIGHT = 30
const THUMB = 26
const TRAVEL = WIDTH - THUMB - 4

/**
 * An on/off switch drawn in the app's own ink rather than Android's: a pill
 * that fills dark when on, a white knob sliding across it, a tick of the phone
 * as it flips.
 */
export function Toggle({ value, onValueChange, disabled }: { value: boolean; onValueChange: (value: boolean) => void; disabled?: boolean }) {
  const colors = useColors()
  // White on the dark fill by day; by night the fill is light, so the knob is dark on it and grey off it.
  const dark = useColorScheme() === 'dark'
  const knobOff = dark ? colors.text2 : '#ffffff'
  const knobOn = dark ? colors.accentFg : '#ffffff'
  const trackOff = dark ? 'rgba(255, 255, 255, 0.16)' : 'rgba(0, 0, 0, 0.12)'
  const reduced = useReducedMotion()
  const on = useSharedValue(value ? 1 : 0)

  useEffect(() => {
    on.value = withTiming(value ? 1 : 0, { duration: reduced ? 0 : 180 })
  }, [value, reduced, on])

  const track = useAnimatedStyle(() => ({ backgroundColor: interpolateColor(on.value, [0, 1], [trackOff, colors.accent]) }))
  const thumb = useAnimatedStyle(() => ({
    backgroundColor: interpolateColor(on.value, [0, 1], [knobOff, knobOn]),
    transform: [{ translateX: 2 + TRAVEL * on.value }],
  }))

  return (
    <Pressable
      accessibilityRole="switch"
      accessibilityState={{ checked: value, disabled }}
      disabled={disabled}
      hitSlop={8}
      onPress={() => {
        void Haptics.selectionAsync()
        onValueChange(!value)
      }}
      style={disabled && styles.dim}
    >
      <Animated.View style={[styles.track, track]}>
        <Animated.View style={[styles.thumb, thumb]} />
      </Animated.View>
    </Pressable>
  )
}

const styles = StyleSheet.create({
  track: { width: WIDTH, height: HEIGHT, borderRadius: HEIGHT / 2, justifyContent: 'center' },
  thumb: { width: THUMB, height: THUMB, borderRadius: THUMB / 2, boxShadow: '0 1px 3px rgba(0, 0, 0, 0.25)' },
  dim: { opacity: 0.4 },
})
