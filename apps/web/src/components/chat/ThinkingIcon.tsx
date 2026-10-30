import type { SVGProps } from 'react'

import { THINKING_ICON_PATHS } from '@openchat/chat-core/thinking-icon'

/** The thinking-effort mark (chat-core/thinking-icon.ts), drawn as lucide draws its icons so it takes the same CSS. */
export function ThinkingIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      {...props}
    >
      {THINKING_ICON_PATHS.map((d) => (
        <path key={d} d={d} />
      ))}
    </svg>
  )
}
