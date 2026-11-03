import type { PluginCodeProgress } from '@openchat/api-client'
import i18next from 'i18next'
import { describe, expect, it } from 'vitest'

import zh from '../locales/zh/common.json'
import { applyBatch, type ConversationLive } from './run-state'
import { isPluginTool, pluginProgressOf, pluginStepLabel, waitingCalls } from './plugin-code'
import { batch, liveRun, run, user } from './testing'

const t = i18next.createInstance()
void t.init({ lng: 'zh', resources: { zh: { common: zh } }, defaultNS: 'common', interpolation: { escapeValue: false } })
const tr = t.t.bind(t) as Parameters<typeof pluginStepLabel>[0]

const progress = (calls: PluginCodeProgress['calls']): PluginCodeProgress => ({
  calls,
  plugins: [{ slug: 'github', name: 'GitHub', iconText: '', iconColor: '#181717', iconPath: '' }],
})

describe('plugin calls', () => {
  it('knows a script and a directly listed tool', () => {
    expect(isPluginTool('plugin_code')).toBe(true)
    expect(isPluginTool('github__create_issue')).toBe(true)
    expect(isPluginTool('web_search')).toBe(false)
  })

  it('reads the result before the live progress, and finds the calls that wait', () => {
    const live = progress([
      { seq: 1, plugin: 'github', tool: 'get_issue', status: 'done' },
      { seq: 2, plugin: 'github', tool: 'create_issue', status: 'waiting', confirmationId: 'c1', arguments: { title: 'x' } },
    ])
    const done = progress([{ seq: 1, plugin: 'github', tool: 'get_issue', status: 'done' }])
    expect(pluginProgressOf(done, live)).toBe(done)
    expect(pluginProgressOf(undefined, live)).toBe(live)
    expect(pluginProgressOf({ other: 1 }, 'nothing')).toBeNull()
    expect(waitingCalls(live).map((c) => c.confirmationId)).toEqual(['c1'])
  })

  it('names the call in flight, or sums up the calls', () => {
    const waiting = progress([{ seq: 1, plugin: 'github', tool: 'create_issue', status: 'waiting', confirmationId: 'c1' }])
    expect(pluginStepLabel(tr, 'plugin_code', waiting, true, false)).toBe('等你允许 GitHub · create_issue')
    expect(pluginStepLabel(tr, 'plugin_code', null, true, false)).toBe('正在使用插件')
    const done = progress([
      { seq: 1, plugin: 'github', tool: 'get_issue', status: 'done' },
      { seq: 2, plugin: 'github', tool: 'get_issue', status: 'done' },
    ])
    expect(pluginStepLabel(tr, 'plugin_code', done, false, false)).toBe('用了 GitHub，调用 2 次')
    expect(pluginStepLabel(tr, 'plugin_code', done, false, true)).toBe('GitHub 的调用没有完成')
  })

  it('keeps what a running call reports, from the snapshot and from events', () => {
    const r = run('r1')
    const held: ConversationLive = { run: liveRun(r, [user('q')]) }
    const reported = progress([{ seq: 1, plugin: 'github', tool: 'get_issue', status: 'running' }])
    const next = applyBatch(held, batch(r, r.version + 1, [{ type: 'tool_progress', callId: 'call-1', progress: reported }]))
    expect(next.run?.progress['call-1']).toEqual(reported)
  })
})
