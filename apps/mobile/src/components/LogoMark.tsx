import { useEffect } from 'react'
import Animated, { Easing, useAnimatedProps, useReducedMotion, useSharedValue, withTiming, type SharedValue } from 'react-native-reanimated'
import Svg, { Circle } from 'react-native-svg'

/**
 * The OpenChat mark (the web's public/logo-mark.svg): six circles sharing one
 * tangent point on the right, brightening outward. In a 32-unit square,
 * stroked in the given colour.
 */
const RINGS = [
  { cx: 29, r: 2, opacity: 0.4 },
  { cx: 26.4, r: 4.6, opacity: 0.52 },
  { cx: 23.8, r: 7.2, opacity: 0.64 },
  { cx: 21.2, r: 9.8, opacity: 0.76 },
  { cx: 18.6, r: 12.4, opacity: 0.88 },
  { cx: 16, r: 15, opacity: 1 },
]

const AnimatedCircle = Animated.createAnimatedComponent(Circle)

/**
 * The mark, drawn once as the page opens: the rings come in from the smallest
 * to the largest, outward from the point they share - the shape of a reply
 * widening from a question. Under reduced motion it is simply there.
 */
export function LogoMark({ size, color }: { size: number; color: string }) {
  const reduced = useReducedMotion()
  const shown = useSharedValue(reduced ? 1 : 0)
  useEffect(() => {
    if (!reduced) shown.value = withTiming(1, { duration: 720, easing: Easing.out(Easing.cubic) })
  }, [reduced, shown])
  return (
    <Svg width={size} height={size} viewBox="0 0 32 32" fill="none" accessibilityElementsHidden importantForAccessibility="no-hide-descendants">
      {RINGS.map((ring, i) => (
        <Ring key={ring.r} ring={ring} index={i} shown={shown} color={color} />
      ))}
    </Svg>
  )
}

function Ring({ ring, index, shown, color }: { ring: (typeof RINGS)[number]; index: number; shown: SharedValue<number>; color: string }) {
  // Each ring's share of the opening: the next starts a little after the last.
  const from = index * 0.1
  const props = useAnimatedProps(() => {
    const t = Math.min(1, Math.max(0, (shown.value - from) / 0.5))
    return { strokeOpacity: ring.opacity * t }
  })
  return <AnimatedCircle cx={ring.cx} cy={16} r={ring.r} stroke={color} strokeWidth={1.3} animatedProps={props} />
}
