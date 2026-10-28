import { useId } from 'react'

import { colorOf } from '@/lib/subagents'
import { cn } from '@/lib/utils'

/** One petal, pointing up; the other three are it turned about the centre. */
const PETAL = 'M12 2.2C14.9 2.2 16.4 4.6 16.4 7.2C16.4 9.2 14.4 10.4 12 10.4C9.6 10.4 7.6 9.2 7.6 7.2C7.6 4.6 9.1 2.2 12 2.2Z'

/**
 * How a sub-agent's mark is drawn: turning while it works, a still bud while
 * it waits for a slot, still and full once done, pale when it failed or was
 * stopped.
 */
export type MarkState = 'running' | 'queued' | 'done' | 'faded'

/**
 * A sub-agent's mark: four petals round an open centre, in a colour fixed by
 * its id, so the one in the reply, the summary and the panel read as the same
 * sub-agent. While it works the flower turns and its petals breathe; it is not
 * a loading indicator (its state is also said in words), so it stands still
 * where motion is reduced.
 */
export function SubagentAvatar({ id, state = 'done', size = 18 }: { id: string; state?: MarkState; size?: number }) {
  // The centre is cut out rather than painted over, so it shows whatever is
  // behind it - a hovered row, the summary's card.
  const hole = `n-subagent-hole-${useId().replace(/:/g, '')}`
  return (
    <svg
      className={cn('n-subagent-avatar', `is-${state}`)}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      aria-hidden="true"
      style={{ ['--n-subagent-color' as string]: `var(--n-agent-${colorOf(id) + 1})` }}
    >
      <mask id={hole} maskUnits="userSpaceOnUse" x="0" y="0" width="24" height="24">
        <rect width="24" height="24" fill="#fff" />
        <circle cx="12" cy="12" r="2.1" fill="#000" />
      </mask>
      <g mask={`url(#${hole})`}>
        <g className="n-subagent-bloom">
          {[0, 90, 180, 270].map((r) => (
            <g key={r} transform={`rotate(${r} 12 12)`}>
              <path className="n-subagent-petal" d={PETAL} />
            </g>
          ))}
        </g>
      </g>
    </svg>
  )
}

/**
 * The sub-agents' button mark: the same flower in one ink, fading from the
 * text colour at the top left to a pale tint of it at the bottom right, so it
 * is black to grey in the light theme and white to grey in the dark, and no
 * single sub-agent's colour stands for all of them.
 */
export function SubagentsIcon({ size = 16 }: { size?: number }) {
  const id = useId().replace(/:/g, '')
  return (
    <svg className="n-subagents-icon" width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
      <defs>
        <linearGradient id={`n-subagents-ink-${id}`} gradientUnits="userSpaceOnUse" x1="4" y1="4" x2="20" y2="20">
          <stop offset="0" stopColor="currentColor" />
          <stop offset="1" stopColor="currentColor" stopOpacity="0.28" />
        </linearGradient>
        <mask id={`n-subagents-hole-${id}`} maskUnits="userSpaceOnUse" x="0" y="0" width="24" height="24">
          <rect width="24" height="24" fill="#fff" />
          <circle cx="12" cy="12" r="2.1" fill="#000" />
        </mask>
      </defs>
      <g mask={`url(#n-subagents-hole-${id})`} fill={`url(#n-subagents-ink-${id})`}>
        {[0, 90, 180, 270].map((r) => (
          <path key={r} d={PETAL} transform={`rotate(${r} 12 12)`} />
        ))}
      </g>
    </svg>
  )
}

/** Several marks overlapping a little, as the launch line and the summary show them. */
export function SubagentAvatars({ ids, state }: { ids: string[]; state?: (id: string) => MarkState }) {
  return (
    <span className="n-subagent-avatars">
      {ids.map((id) => (
        <SubagentAvatar key={id} id={id} state={state?.(id) ?? 'done'} />
      ))}
    </span>
  )
}

export default SubagentAvatar
