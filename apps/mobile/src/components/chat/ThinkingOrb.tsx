import MaskedViewClass from '@react-native-masked-view/masked-view'
import { useEffect, useState, type ComponentType, type ReactElement, type ReactNode } from 'react'
import { StyleSheet, Text, View, type StyleProp, type TextStyle, type ViewStyle } from 'react-native'
import Animated, { Easing, useAnimatedStyle, useSharedValue, withRepeat, withTiming, type SharedValue } from 'react-native-reanimated'
import Svg, { Defs, LinearGradient, Rect, Stop } from 'react-native-svg'

import { useColors } from '@/theme'

const DOT = 3
const GAP = 2
/** One wave across the dots, and how far apart two neighbouring diagonals light up (the web's n-orb-wave). */
const CYCLE = 1320
const STEP = 110

/**
 * Nine dots in a square, lighting up in a wave from one corner to the other:
 * the mark for "the model is working on it" (the web's ThinkingOrb) - before
 * its first word, while it thinks, while a step runs. It keeps moving under
 * reduced motion, as the web's does: a still indicator reads as a stuck one.
 */
export function ThinkingOrb({ color }: { color?: string }) {
  const colors = useColors()
  const clock = useSharedValue(0)
  useEffect(() => {
    clock.value = withRepeat(withTiming(1, { duration: CYCLE, easing: Easing.linear }), -1, false)
  }, [clock])
  return (
    <View style={styles.orb} accessibilityElementsHidden importantForAccessibility="no-hide-descendants">
      {Array.from({ length: 9 }, (_, i) => (
        // The wave runs along the anti-diagonals: row + column.
        <Dot key={i} clock={clock} delay={((Math.floor(i / 3) + (i % 3)) * STEP) / CYCLE} color={color ?? colors.text} />
      ))}
    </View>
  )
}

function Dot({ clock, delay, color }: { clock: SharedValue<number>; delay: number; color: string }) {
  const style = useAnimatedStyle(() => {
    const t = (clock.value - delay + 1) % 1
    const ease = (x: number) => (x < 0.5 ? 2 * x * x : 1 - (-2 * x + 2) ** 2 / 2)
    // Dim, lit at 22% of the wave, dim again by 55% and resting till the next.
    const opacity = t < 0.22 ? 0.18 + 0.82 * ease(t / 0.22) : t < 0.55 ? 1 - 0.82 * ease((t - 0.22) / 0.33) : 0.18
    return { opacity }
  })
  return <Animated.View style={[styles.dot, { backgroundColor: color }, style]} />
}

/**
 * Words for what the model is doing, a highlight sweeping across them (the
 * web's n-reasoning-live): the quiet colour, the body's colour passing left to
 * right every two seconds. The text is a mask over a gradient two and a half
 * times its width, slid across it.
 */
export function LiveText({ text, style }: { text: string; style?: StyleProp<TextStyle> }) {
  const colors = useColors()
  const [width, setWidth] = useState(0)
  const sweep = useSharedValue(0)
  useEffect(() => {
    if (!width) return
    sweep.value = 0
    sweep.value = withRepeat(withTiming(1, { duration: 2000, easing: Easing.linear }), -1, false)
  }, [sweep, width])
  const band = useAnimatedStyle(() => ({ transform: [{ translateX: -1.5 * width * (1 - sweep.value) }] }))
  const words = (color?: string) => (
    <Text style={[style, color ? { color } : null]} numberOfLines={1}>
      {text}
    </Text>
  )
  return (
    <MaskedView style={styles.live} maskElement={words()}>
      {/* Sizes the box, and shows in the quiet colour until the gradient is laid. */}
      <View onLayout={(event) => setWidth(event.nativeEvent.layout.width)}>{words(colors.text3)}</View>
      {width ? (
        <Animated.View style={[styles.band, { width: width * 2.5 }, band]}>
          <Svg width={width * 2.5} height="100%">
            <Defs>
              <LinearGradient id="live" x1="0" y1="0" x2="1" y2="0">
                <Stop offset="0" stopColor={colors.text3} />
                <Stop offset="0.4" stopColor={colors.text3} />
                <Stop offset="0.5" stopColor={colors.text} />
                <Stop offset="0.6" stopColor={colors.text3} />
                <Stop offset="1" stopColor={colors.text3} />
              </LinearGradient>
            </Defs>
            <Rect width="100%" height="100%" fill="url(#live)" />
          </Svg>
        </Animated.View>
      ) : null}
    </MaskedView>
  )
}

// The library types its class against another copy of React's types, which
// this one's JSX does not accept; the props are as documented.
const MaskedView = MaskedViewClass as unknown as ComponentType<{ style?: StyleProp<ViewStyle>; maskElement: ReactElement; children: ReactNode }>

const styles = StyleSheet.create({
  orb: { width: DOT * 3 + GAP * 2, flexDirection: 'row', flexWrap: 'wrap', gap: GAP },
  dot: { width: DOT, height: DOT, borderRadius: DOT / 2 },
  live: { flexShrink: 1 },
  band: { position: 'absolute', top: 0, bottom: 0, left: 0 },
})
