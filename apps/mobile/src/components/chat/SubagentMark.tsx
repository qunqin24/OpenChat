import { useEffect } from 'react'
import Animated, { useAnimatedStyle, useReducedMotion, useSharedValue, withRepeat, withTiming, Easing, cancelAnimation } from 'react-native-reanimated'
import Svg, { G, Path } from 'react-native-svg'

import { colorOf } from '@openchat/chat-core/subagents'

import { useColors } from '@/theme'

export type MarkState = 'running' | 'queued' | 'done' | 'faded'

const PETAL = 'M12 2.2C14.9 2.2 16.4 4.6 16.4 7.2C16.4 9.2 14.4 10.4 12 10.4C9.6 10.4 7.6 9.2 7.6 7.2C7.6 4.6 9.1 2.2 12 2.2Z'

/**
 * A sub-agent's mark (the web's SubagentAvatar): four petals in a colour fixed
 * by its id, so the one in the reply and the one in the panel read as the
 * same. While it works the flower turns; it stands still where motion is
 * reduced (its state is also said in words). Paler once it failed or was
 * stopped, faint while it waits its turn.
 */
export function SubagentMark({ id, state = 'done', size = 18 }: { id: string; state?: MarkState; size?: number }) {
  const colors = useColors()
  const reduced = useReducedMotion()
  const turn = useSharedValue(0)
  const running = state === 'running' && !reduced
  useEffect(() => {
    if (running) turn.value = withRepeat(withTiming(360, { duration: 3200, easing: Easing.linear }), -1)
    else {
      cancelAnimation(turn)
      turn.value = 0
    }
  }, [running, turn])
  const spin = useAnimatedStyle(() => ({ transform: [{ rotate: `${turn.value}deg` }] }))
  const color = colors.agents[colorOf(id) % colors.agents.length]!
  return (
    <Animated.View style={[{ width: size, height: size, opacity: state === 'faded' ? 0.45 : state === 'queued' ? 0.6 : 1 }, spin]}>
      <Svg width={size} height={size} viewBox="0 0 24 24">
        <G>
          {[0, 90, 180, 270].map((r) => (
            <Path key={r} d={PETAL} fill={color} transform={`rotate(${r} 12 12)`} />
          ))}
        </G>
      </Svg>
    </Animated.View>
  )
}
