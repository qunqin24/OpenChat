import type { ReasoningEffort } from '@openchat/api-client'
import MaskedViewClass from '@react-native-masked-view/masked-view'
import { BlurView } from 'expo-blur'
import * as Haptics from 'expo-haptics'
import { useEffect, useState, type ComponentRef, type ComponentType, type ReactElement, type ReactNode, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'
import { BackHandler, Pressable, StyleSheet, Text, useColorScheme, useWindowDimensions, View, type StyleProp, type ViewStyle } from 'react-native'
import { GestureDetector, usePanGesture } from 'react-native-gesture-handler'
import Animated, { Easing, runOnJS, useAnimatedProps, useAnimatedStyle, useReducedMotion, useSharedValue, withSpring, withTiming, type SharedValue } from 'react-native-reanimated'
import { useReanimatedKeyboardAnimation } from 'react-native-keyboard-controller'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import Svg, { Defs, LinearGradient, Rect, Stop } from 'react-native-svg'

import { useColors } from '@/theme'

/** How long the layer takes to come or go, in ms. */
const FADE_MS = 180
const TRACK_HEIGHT = 64
const KNOB = 46
/** From the track's edge to the knob's, at either end. */
const INSET = 9
const MAX_TRACK = 320
const SPRING = { damping: 18, stiffness: 260, mass: 0.8 }
/** How much of the screen, from the foot, the blur covers; the rest stays clear. */
const REGION_SHARE = 0.55
const REGION_MAX = 480
/** Where the capsule stands from the foot of the screen: above the composer, or with the keyboard up, just above the keyboard. */
const STAGE_BOTTOM = 112
const STAGE_OVER_KEYBOARD = 24
/** How hard the blur is where it is full, at the foot of the region. */
const BLUR = 55

interface Props {
  open: boolean
  /** "auto" first, then the model's levels, lowest to highest. */
  levels: ReasoningEffort[]
  value: ReasoningEffort
  onPick: (effort: ReasoningEffort) => void
  onClose: () => void
  /** The page to blur behind the layer (expo-blur's BlurTargetView on Android). */
  blurTarget: RefObject<ComponentRef<typeof View> | null>
}

/**
 * Thinking effort as a capsule over the page, whose lower part blurs: ink fills from the left
 * up to the knob, which a drag or a tap moves to the nearest level - the
 * higher, the fuller. The level's name stands above it; each new level ticks
 * the phone, and letting go sets it. A tap anywhere else, or back, closes.
 *
 * Not a Modal: on Android a blur sees only its own window, so this lies over
 * the chat in the chat's own window.
 */
export function EffortSlider({ open, levels, value, onPick, onClose, blurTarget }: Props) {
  const reduced = useReducedMotion()
  // Still on screen while it fades out after `open` turned false.
  const [mounted, setMounted] = useState(open)
  const shown = useSharedValue(0)
  if (open && !mounted) setMounted(true)

  useEffect(() => {
    if (!mounted) return
    shown.value = withTiming(open ? 1 : 0, { duration: reduced ? 0 : FADE_MS, easing: Easing.out(Easing.cubic) }, (finished) => {
      if (finished && !open) runOnJS(setMounted)(false)
    })
  }, [open, mounted, reduced, shown])

  useEffect(() => {
    if (!open) return
    const subscription = BackHandler.addEventListener('hardwareBackPress', () => {
      onClose()
      return true
    })
    return () => subscription.remove()
  }, [open, onClose])

  const fade = useAnimatedStyle(() => ({ opacity: shown.value }))
  // Ahead of the backdrop, and drawing in a little: white on a white wash, the
  // capsule faded at the backdrop's pace left its dark knob floating behind.
  const rise = useAnimatedStyle(() => ({
    opacity: shown.value * shown.value,
    transform: [{ translateY: (1 - shown.value) * 12 }, { scale: 0.96 + 0.04 * shown.value }],
  }))

  // The keyboard may be up (the message being written stays open): the blur
  // and the capsule ride on top of it, and the capsule comes down over the
  // composer, which is blurred with the page.
  const keyboard = useReanimatedKeyboardAnimation()
  const aboveKeyboard = useAnimatedStyle(() => ({ bottom: -keyboard.height.value }))
  const stageAt = useAnimatedStyle(() => ({ bottom: STAGE_BOTTOM - (STAGE_BOTTOM - STAGE_OVER_KEYBOARD) * keyboard.progress.value - keyboard.height.value }))

  if (!mounted) return null
  return (
    <View style={StyleSheet.absoluteFill} pointerEvents={open ? 'auto' : 'none'}>
      <Animated.View style={[StyleSheet.absoluteFill, fade]}>
        <Backdrop blurTarget={blurTarget} onClose={onClose} shown={shown} lift={aboveKeyboard} />
      </Animated.View>
      <Animated.View style={[styles.stage, stageAt, rise]} pointerEvents="box-none">
        <Track levels={levels} value={value} onPick={onPick} />
      </Animated.View>
    </View>
  )
}

/**
 * The lower part of the page, blurred and faded the lower it is - the top of
 * the chat stays clear, as in a progressive blur. One blur, cut by a gradient
 * mask so it comes in by degrees rather than at a line (blur in bands, without
 * the mask, showed each band's edge), under a wash of the page's own colour
 * that thickens with it: Android's blur comes out a shade grey and grainy, and
 * the wash keeps that off a blank page. The whole page still closes the slider
 * on a tap.
 */
function Backdrop({ blurTarget, onClose, shown, lift }: { blurTarget: Props['blurTarget']; onClose: () => void; shown: SharedValue<number>; lift: ReturnType<typeof useAnimatedStyle> }) {
  const { t } = useTranslation()
  const colors = useColors()
  const dark = useColorScheme() === 'dark'
  const window = useWindowDimensions()
  const height = Math.min(window.height * REGION_SHARE, REGION_MAX)
  // The blur's strength comes and goes with the layer: Android's blur ignores
  // the opacity of the views around it, so fading the layer alone left the
  // blur standing, grey, until the layer was gone.
  const strength = useAnimatedProps(() => ({ intensity: BLUR * shown.value }))
  return (
    <>
      <Pressable style={StyleSheet.absoluteFill} onPress={onClose} accessibilityRole="button" accessibilityLabel={t('settings.close')} />
      <Animated.View pointerEvents="none" style={[styles.region, { height }, lift]}>
        {/* Android 12 and later blur; older ones get the wash alone. */}
        <MaskedView style={StyleSheet.absoluteFill} maskElement={<Ramp id="mask" color="#000" stops={[[0, 0], [0.55, 1], [1, 1]]} />}>
          <AnimatedBlurView
            style={StyleSheet.absoluteFill}
            blurTarget={blurTarget}
            blurMethod="dimezisBlurViewSdk31Plus"
            intensity={BLUR}
            animatedProps={strength}
            tint={dark ? 'dark' : 'systemChromeMaterialLight'}
          />
        </MaskedView>
        <Ramp id="wash" color={colors.bg} stops={[[0, 0], [0.55, 0.6], [1, 0.85]]} />
      </Animated.View>
    </>
  )
}

const AnimatedBlurView = Animated.createAnimatedComponent(BlurView)

// The library types its class against another copy of React's types, which
// this one's JSX does not accept; the props are as documented.
const MaskedView = MaskedViewClass as unknown as ComponentType<{ style?: StyleProp<ViewStyle>; maskElement: ReactElement; children: ReactNode }>

/** A colour from clear at the top to the given opacities going down: [offset, opacity] pairs. */
function Ramp({ id, color, stops }: { id: string; color: string; stops: [number, number][] }) {
  return (
    <Svg style={StyleSheet.absoluteFill} width="100%" height="100%">
      <Defs>
        <LinearGradient id={id} x1="0" y1="0" x2="0" y2="1">
          {stops.map(([offset, opacity]) => (
            <Stop key={offset} offset={offset} stopColor={color} stopOpacity={opacity} />
          ))}
        </LinearGradient>
      </Defs>
      <Rect x="0" y="0" width="100%" height="100%" fill={`url(#${id})`} />
    </Svg>
  )
}

function Track({ levels, value, onPick }: Pick<Props, 'levels' | 'value' | 'onPick'>) {
  const { t } = useTranslation()
  const colors = useColors()
  const reduced = useReducedMotion()
  const window = useWindowDimensions()
  const insets = useSafeAreaInsets()
  const width = Math.min(MAX_TRACK, window.width - 64)
  const span = width - INSET * 2 - KNOB
  const count = levels.length
  const stopAt = (i: number) => INSET + (span * i) / Math.max(1, count - 1)

  const start = Math.max(0, levels.indexOf(value))
  const [index, setIndex] = useState(start)
  const current = useSharedValue(start)
  const x = useSharedValue(stopAt(start))

  const moved = (i: number) => {
    setIndex(i)
    void Haptics.selectionAsync()
  }
  const chosen = (i: number) => {
    const level = levels[i]
    if (level && level !== value) onPick(level)
  }

  const move = (to: number) => {
    'worklet'
    const i = Math.round(Math.min(1, Math.max(0, (to - INSET - KNOB / 2) / span)) * (count - 1))
    if (i === current.value) return
    current.value = i
    const target = INSET + (span * i) / Math.max(1, count - 1)
    x.value = reduced ? target : withSpring(target, SPRING)
    runOnJS(moved)(i)
  }

  const pan = usePanGesture({
    minDistance: 0,
    onBegin: (event) => {
      'worklet'
      move(event.x)
    },
    onUpdate: (event) => {
      'worklet'
      move(event.x)
    },
    onFinalize: () => {
      'worklet'
      runOnJS(chosen)(current.value)
    },
  })

  const knob = useAnimatedStyle(() => ({ transform: [{ translateX: x.value }] }))
  const fill = useAnimatedStyle(() => ({ width: x.value + KNOB - 3 }))

  const step = (by: number) => {
    const i = Math.min(count - 1, Math.max(0, index + by))
    current.set(i)
    x.set(stopAt(i))
    setIndex(i)
    chosen(i)
  }
  const name = (level: ReasoningEffort | undefined) => (level ? t(`chat.thinking.${level}`) : '')

  return (
    <View style={[styles.center, { marginBottom: insets.bottom }]}>
      <Text style={[styles.label, { color: colors.text }]}>{name(levels[index])}</Text>
      <GestureDetector gesture={pan}>
        <View
          accessible
          accessibilityRole="adjustable"
          accessibilityLabel={t('chat.thinking.heading')}
          accessibilityValue={{ text: name(levels[index]) }}
          accessibilityActions={[{ name: 'increment' }, { name: 'decrement' }]}
          onAccessibilityAction={(event) => step(event.nativeEvent.actionName === 'increment' ? 1 : -1)}
          style={[styles.track, { width, backgroundColor: colors.card, borderColor: colors.line }]}
        >
          <Animated.View style={[styles.fill, { backgroundColor: colors.accent }, fill]} />
          {levels.map((level, i) => (
            <View
              key={level}
              pointerEvents="none"
              style={[styles.dot, { left: stopAt(i) + KNOB / 2 - 4, backgroundColor: i <= index ? colors.accentFg : colors.text3, opacity: i <= index ? 0.4 : 0.5 }]}
            />
          ))}
          <Animated.View pointerEvents="none" style={[styles.knob, { backgroundColor: colors.accentFg }, knob]} />
        </View>
      </GestureDetector>
    </View>
  )
}

const styles = StyleSheet.create({
  // Just above the composer, which stays visible under the blur.
  stage: { position: 'absolute', left: 0, right: 0 },
  region: { position: 'absolute', left: 0, right: 0, bottom: 0 },
  center: { alignItems: 'center' },
  label: { fontSize: 22, lineHeight: 28, fontWeight: '700', marginBottom: 16 },
  track: {
    height: TRACK_HEIGHT,
    borderRadius: TRACK_HEIGHT / 2,
    borderWidth: StyleSheet.hairlineWidth,
    // boxShadow, not elevation: Android draws an elevation shadow apart from
    // the view, and it stayed behind as a grey smudge while the capsule faded.
    boxShadow: '0 8px 24px rgba(0, 0, 0, 0.12)',
  },
  fill: { position: 'absolute', left: 6, top: 6, bottom: 6, borderRadius: (TRACK_HEIGHT - 12) / 2 },
  dot: { position: 'absolute', top: TRACK_HEIGHT / 2 - 4, width: 8, height: 8, borderRadius: 4 },
  knob: {
    position: 'absolute',
    top: (TRACK_HEIGHT - KNOB) / 2,
    left: 0,
    width: KNOB,
    height: KNOB,
    borderRadius: KNOB / 2,
    boxShadow: '0 2px 6px rgba(0, 0, 0, 0.25)',
  },
})
