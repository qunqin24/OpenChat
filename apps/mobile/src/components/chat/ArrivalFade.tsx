import { useEffect, useRef, useState, type ReactNode } from 'react'
import {
  AccessibilityInfo,
  AppState,
  StyleSheet,
  Text,
  View,
  type StyleProp,
  type TextLayoutEvent,
  type TextProps,
  type TextStyle,
} from 'react-native'

import { coverOpacity, nextArrivals, noArrivals, type ArrivalBatch, type ArrivalState } from '@openchat/chat-core/arrival'

import { useColors } from '@/theme'

let reduceMotion = false
void AccessibilityInfo.isReduceMotionEnabled().then((on) => (reduceMotion = on))
AccessibilityInfo.addEventListener('reduceMotionChanged', (on) => (reduceMotion = on))

type Line = TextLayoutEvent['nativeEvent']['lines'][number]

/** A cover over part of a line of a batch still fading in. */
interface Cover {
  x: number
  y: number
  width: number
  height: number
  born: number
}

/**
 * Covers for the lines each batch lies on. React Native says where each line
 * is and what it holds, not where a character is: a line the batch fills is
 * covered whole, and the part of one it starts or ends inside is placed by its
 * share of the line's characters.
 */
function coversOf(lines: Line[], batches: ArrivalBatch[]): Cover[] {
  if (batches.length === 0) return []
  const covers: Cover[] = []
  let offset = 0
  for (const line of lines) {
    const start = offset
    const length = line.text.length
    offset += length
    if (length === 0) continue
    for (const batch of batches) {
      const a = Math.max(batch.start, start) - start
      const b = Math.min(batch.end, start + length) - start
      if (a >= b || !line.text.slice(a, b).trim()) continue
      const left = line.x + (line.width * a) / length
      const right = line.x + (line.width * b) / length
      covers.push({ x: Math.floor(left), y: Math.floor(line.y), width: Math.ceil(right - left) + 1, height: Math.ceil(line.height), born: batch.born })
    }
  }
  return covers
}

interface Props extends Omit<TextProps, 'onTextLayout' | 'style'> {
  style?: StyleProp<TextStyle>
  children: ReactNode
  /** The text arrived while the reader watched: even what it holds at first fades in. */
  fresh?: boolean
}

/**
 * Text shown as it arrives, each batch fading in together (the web's
 * useArrivalFade, arrival-fade.ts; what a batch is and how it fades are
 * chat-core/arrival.ts). Nothing in the text is touched: covers in the page's
 * colour lie over the lines of the batches of the last moment, going from
 * opaque to clear - a handful of views, however long the answer, never a view
 * per character (v1.1.0's mistake). Reduced motion, and an app in the
 * background, get the text as it is.
 */
export function FadingText({ children, fresh = false, style, ...rest }: Props) {
  const colors = useColors()
  const arrivals = useRef<ArrivalState>(noArrivals)
  const [covers, setCovers] = useState<Cover[]>([])

  const laidOut = (event: TextLayoutEvent) => {
    const lines = event.nativeEvent.lines
    const length = lines.reduce((sum, line) => sum + line.text.length, 0)
    const before = arrivals.current.batches.length
    arrivals.current = nextArrivals(arrivals.current, length, performance.now(), {
      fresh,
      fading: !reduceMotion && AppState.currentState === 'active',
    })
    if (before === 0 && arrivals.current.batches.length === 0) return
    setCovers(coversOf(lines, arrivals.current.batches))
  }

  return (
    <View>
      <Text {...rest} style={style} onTextLayout={laidOut}>
        {children}
      </Text>
      {covers.length > 0 ? <Covers covers={covers} surface={colors.bg} onDone={() => setCovers([])} /> : null}
    </View>
  )
}

/** The covers, fading frame by frame; gone once all are clear. */
function Covers({ covers, surface, onDone }: { covers: Cover[]; surface: string; onDone: () => void }) {
  const [now, setNow] = useState(() => performance.now())
  const done = useRef(onDone)
  useEffect(() => {
    done.current = onDone
  })
  useEffect(() => {
    let frame = 0
    const tick = () => {
      const at = performance.now()
      setNow(at)
      if (covers.some((cover) => coverOpacity(cover.born, at) > 0)) frame = requestAnimationFrame(tick)
      else done.current()
    }
    frame = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame)
  }, [covers])
  return (
    <View pointerEvents="none" style={StyleSheet.absoluteFill}>
      {covers.map((cover, i) => (
        <View
          key={i}
          style={[styles.cover, { left: cover.x, top: cover.y, width: cover.width, height: cover.height, backgroundColor: surface, opacity: coverOpacity(cover.born, now) }]}
        />
      ))}
    </View>
  )
}

const styles = StyleSheet.create({ cover: { position: 'absolute' } })
