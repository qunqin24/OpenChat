import type { ActivityItem } from '@openchat/chat-core/tool-activity'

import type { OrbLook } from './orb-core'

/** Before the first word, with nothing yet to say what the model is at. */
export const WORKING: OrbLook = { state: 'working' }
export const RETRYING: OrbLook = { state: 'retrying' }
export const COMPACTING: OrbLook = { state: 'compacting' }
/** A conversation left working, in the sidebar: the lightest orb, as several may run at once. */
export const BACKGROUND: OrbLook = { state: 'background' }
/** Thinking at rest, drawn still: on the process's line once the answer has come, and on the thinking picker. */
export const THOUGHT: OrbLook = { state: 'idle' }
/** The model rewriting a document, on its card. */
export const WRITING: OrbLook = { state: 'compacting', variant: 'fuse' }

/**
 * The orb for a call in flight, by what it does. Using and Using · Type draw
 * their pictures in a thousand dots, against eighty for the rest, so they go
 * only where one orb is ever moving: the activity line, not the sidebar.
 */
export function toolOrb(name: string): OrbLook {
  if (name === 'web_search') return { state: 'searching' }
  if (name === 'web_fetch') return { state: 'using', variant: 'scroll' }
  if (name === 'file_read' || name === 'skill') return { state: 'using', variant: 'zoom' }
  if (name === 'file_search') return { state: 'searching', variant: 'lighthouse' }
  if (name === 'memory' || name === 'document') return { state: 'using', variant: 'type' }
  if (name === 'ask_user') return { state: 'waiting' }
  if (name.startsWith('subagent')) return { state: 'background', variant: 'spiral' }
  return { state: 'using', variant: 'tap' }
}

/**
 * The orb on an activity line while the model works, matching what its title
 * says (activityHead): a retry first, then the call in flight it names (the
 * first, when several run at once), then thinking, or quiet between a round
 * and the next response.
 */
export function activityOrb(items: ActivityItem[], thinking: 'reasoning' | 'waiting' | 'retrying' | false): OrbLook {
  if (thinking === 'retrying') return RETRYING
  const active = items.find(
    (item): item is ActivityItem & { kind: 'tool' } => item.kind === 'tool' && (item.status === 'pending' || item.status === 'running'),
  )
  if (active) return toolOrb(active.call.name)
  if (thinking === 'reasoning') return { state: 'reasoning' }
  return { state: 'working', variant: 'gyro' }
}
