import type { ActivityItem, ToolStatus } from '@openchat/chat-core/tool-activity'
import { describe, expect, it } from 'vitest'

import { activityOrb, toolOrb } from './activity-orb'

const step = (name: string, status: ToolStatus, key = name): ActivityItem =>
  ({ kind: 'tool', key, status, call: { type: 'tool_call', id: key, name, arguments: {} } }) as ActivityItem
const thought: ActivityItem = { kind: 'reasoning', key: 'r', text: 'hmm' }

describe('toolOrb', () => {
  it('gives each kind of call its own look', () => {
    expect(toolOrb('web_search')).toEqual({ state: 'searching' })
    expect(toolOrb('web_fetch')).toEqual({ state: 'using', variant: 'scroll' })
    expect(toolOrb('file_search')).toEqual({ state: 'searching', variant: 'lighthouse' })
    expect(toolOrb('ask_user')).toEqual({ state: 'waiting' })
    expect(toolOrb('subagent_message')).toEqual({ state: 'background', variant: 'spiral' })
  })

  it('taps for a call it has no look for', () => {
    expect(toolOrb('scheduled_tasks')).toEqual({ state: 'using', variant: 'tap' })
    expect(toolOrb('something_new')).toEqual({ state: 'using', variant: 'tap' })
  })
})

describe('activityOrb', () => {
  it('says a retry over everything else', () => {
    expect(activityOrb([step('web_search', 'running')], 'retrying')).toEqual({ state: 'retrying' })
  })

  it('follows the call the title names: the first in flight', () => {
    const items = [step('web_search', 'succeeded'), step('web_fetch', 'running'), step('file_read', 'pending')]
    expect(activityOrb(items, false)).toEqual({ state: 'using', variant: 'scroll' })
  })

  it('reasons while a thought streams, and turns between rounds', () => {
    expect(activityOrb([thought], 'reasoning')).toEqual({ state: 'reasoning' })
    expect(activityOrb([step('web_search', 'succeeded')], 'waiting')).toEqual({ state: 'working', variant: 'gyro' })
  })
})
