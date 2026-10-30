import Svg, { Path } from 'react-native-svg'

import { THINKING_ICON_PATHS } from '@openchat/chat-core/thinking-icon'

/** The thinking-effort mark (chat-core/thinking-icon.ts), sized and stroked as lucide-react-native draws its icons. */
export function ThinkingIcon({ size = 24, color, strokeWidth = 2 }: { size?: number; color: string; strokeWidth?: number }) {
  return (
    <Svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth={strokeWidth} strokeLinecap="round" strokeLinejoin="round">
      {THINKING_ICON_PATHS.map((d) => (
        <Path key={d} d={d} />
      ))}
    </Svg>
  )
}
