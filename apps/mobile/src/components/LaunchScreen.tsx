import { StatusBar } from 'expo-status-bar'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Pressable, StyleSheet, Text, useColorScheme, useWindowDimensions } from 'react-native'
import Animated, {
  Easing,
  runOnJS,
  useAnimatedProps,
  useAnimatedStyle,
  useReducedMotion,
  useSharedValue,
  withTiming,
  type SharedValue,
} from 'react-native-reanimated'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import Svg, { Circle, Defs, LinearGradient, Rect, Stop } from 'react-native-svg'

import { LogoMark } from './LogoMark'

/**
 * The web's sign-in panel (apps/web auth.css, components/auth/Ripples.tsx)
 * as the app's opening, timed as the web times it, in ms: the ink panel grows
 * in from a smaller block (0-700), the rings unfold inside it from a point on
 * its right edge (from 450, one every 40, each over 1800), the name and then
 * the claim fade in (1700, 1900); held a moment, the panel draws back and
 * fades (from 3000), leaving the app. Shown on every cold start, over the
 * system's splash, until the session is known too; a tap ends it early.
 */
const GROW = 700
const UNFOLD_FROM = 450
const UNFOLD_STEP = 40
const UNFOLD = 1800
const BRAND_AT = 1700
const CLAIM_AT = 1900
const APPEAR = 700
const HOLD = 3000
const LEAVE = 600

/** As the mark draws them: all touching one point on the right edge, brightening outward. */
const RINGS = Array.from({ length: 22 }, (_, i) => ({ r: 8 * 1.24 ** i, opacity: 0.3 + 0.62 * (i / 21) }))

const growEase = Easing.bezierFn(0.2, 0.8, 0.2, 1)
// Each ring slows as it spreads, like a ripple on still water.
const rippleEase = Easing.bezierFn(0.12, 0.75, 0.2, 1)

const AnimatedCircle = Animated.createAnimatedComponent(Circle)

export function LaunchScreen({ ready, onShown }: { ready: boolean; onShown: () => void }) {
  const { t } = useTranslation()
  const dark = useColorScheme() === 'dark'
  const window = useWindowDimensions()
  const insets = useSafeAreaInsets()
  const reduced = useReducedMotion()
  const [played, setPlayed] = useState(reduced)
  const [gone, setGone] = useState(false)
  const clock = useSharedValue(0)
  const leave = useSharedValue(0)

  useEffect(() => {
    if (reduced) return
    clock.value = withTiming(HOLD, { duration: HOLD, easing: Easing.linear }, (finished) => {
      if (finished) runOnJS(setPlayed)(true)
    })
  }, [clock, reduced])
  useEffect(() => {
    if (!played || !ready) return
    leave.value = withTiming(1, { duration: reduced ? 1 : LEAVE, easing: Easing.bezier(0.4, 0, 0.2, 1) }, (finished) => {
      if (finished) runOnJS(setGone)(true)
    })
  }, [played, ready, leave, reduced])

  const panel = useAnimatedStyle(() => {
    const grown = growEase(Math.min(1, clock.value / GROW))
    return {
      opacity: (reduced ? 1 : grown) * (1 - leave.value),
      transform: [{ scale: (reduced ? 1 : 0.6 + 0.4 * grown) * (1 - 0.08 * leave.value) }],
    }
  })
  const page = useAnimatedStyle(() => ({ opacity: 1 - Math.max(0, (leave.value - 0.5) * 2) }))
  const brand = useAnimatedStyle(() => ({ opacity: reduced ? 1 : Math.min(1, Math.max(0, (clock.value - BRAND_AT) / APPEAR)) }))
  const claim = useAnimatedStyle(() => ({ opacity: reduced ? 1 : Math.min(1, Math.max(0, (clock.value - CLAIM_AT) / APPEAR)) }))

  if (gone) return null
  // The panel, inset from the screen's edge with rounded corners, as the web's is from the window's.
  const width = window.width - 24
  const height = window.height - 24
  // The rings' point on the right edge, a little below the middle (the web's 560 of 1000), and
  // their sizes in the web's units, scaled to the panel's height.
  const unit = height / 1000
  const pointY = height * 0.56

  return (
    <Animated.View style={[StyleSheet.absoluteFill, { backgroundColor: dark ? '#161616' : '#ffffff' }, page]} onLayout={onShown}>
      <StatusBar style="light" />
      <Pressable style={StyleSheet.absoluteFill} onPress={() => setPlayed(true)} accessibilityLabel={t('app.name')}>
        <Animated.View style={[styles.panel, { width, height, backgroundColor: dark ? '#1f1f1f' : '#0d0d0d' }, panel]}>
          <Svg width={width} height={height} style={StyleSheet.absoluteFill}>
            {RINGS.map((ring, i) => (
              <Ring key={i} clock={clock} reduced={reduced} index={i} radius={ring.r * unit} opacity={ring.opacity} x={width} y={pointY} />
            ))}
            {/* The lower half fades back to the panel, so the claim reads over the rings. */}
            <Defs>
              <LinearGradient id="launch-fade" x1="0" y1="0" x2="0" y2="1">
                <Stop offset="0" stopColor={dark ? '#1f1f1f' : '#0d0d0d'} stopOpacity={0} />
                <Stop offset="0.78" stopColor={dark ? '#1f1f1f' : '#0d0d0d'} stopOpacity={1} />
              </LinearGradient>
            </Defs>
            <Rect x="0" y={height * 0.45} width={width} height={height * 0.55} fill="url(#launch-fade)" />
          </Svg>
          <Animated.View style={[styles.brand, { top: Math.max(28, insets.top + 16) }, brand]}>
            <LogoMark size={22} color="#ffffff" />
            <Text style={styles.brandName}>{t('app.name')}</Text>
          </Animated.View>
          <Animated.View style={[styles.claim, { bottom: Math.max(40, insets.bottom + 28) }, claim]}>
            <Text style={styles.claimLine}>{t('auth.panel.claim')}</Text>
            <Text style={styles.tagline}>{t('app.tagline')}</Text>
          </Animated.View>
        </Animated.View>
      </Pressable>
    </Animated.View>
  )
}

/** One ring, unfolding from the shared point: its radius grows and its centre moves left with it. */
function Ring({
  clock,
  reduced,
  index,
  radius,
  opacity,
  x,
  y,
}: {
  clock: SharedValue<number>
  reduced: boolean
  index: number
  radius: number
  opacity: number
  x: number
  y: number
}) {
  const props = useAnimatedProps(() => {
    const local = reduced ? 1 : Math.min(1, Math.max(0, (clock.value - UNFOLD_FROM - index * UNFOLD_STEP) / UNFOLD))
    const r = radius * rippleEase(local)
    return { r, cx: x - r, strokeOpacity: opacity * Math.min(1, local / 0.12) }
  })
  return <AnimatedCircle cy={y} fill="none" stroke="#ffffff" strokeWidth={1} animatedProps={props} />
}

const styles = StyleSheet.create({
  panel: { position: 'absolute', top: 12, left: 12, borderRadius: 34, overflow: 'hidden' },
  brand: { position: 'absolute', left: 28, flexDirection: 'row', alignItems: 'center', gap: 9 },
  brandName: { color: '#ffffff', fontSize: 16, fontWeight: '600' },
  claim: { position: 'absolute', left: 28, right: 28 },
  claimLine: { color: '#ffffff', fontFamily: 'serif', fontSize: 34, lineHeight: 44, fontWeight: '600' },
  tagline: { marginTop: 14, color: 'rgba(255,255,255,0.55)', fontSize: 14 },
})
