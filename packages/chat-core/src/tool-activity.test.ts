import { describe, expect, it } from 'vitest'

import { activityHead, activitySegments, plainSnippet, stepLabel, type ActivityItem } from './tool-activity'

describe('plainSnippet', () => {
  it('keeps the words of links and drops images and bare addresses', () => {
    expect(plainSnippet('+ [懂车帝](https://www.dongchedi.com/?zt=tt_pc_home_channel)...')).toBe('+ 懂车帝')
    expect(plainSnippet('[](https://www.toutiao.com/)\n...')).toBe('')
    expect(plainSnippet('![logo](https://a.example/x.png) 今日头条 https://www.toutiao.com/a1')).toBe('今日头条')
  })

  it('flattens Markdown marks and runs of space', () => {
    expect(plainSnippet('## 标题\n\n**重点**  内容 | 表格')).toBe('标题 重点 内容 表格')
  })

  it('leaves plain text as it is', () => {
    expect(plainSnippet('国防部新闻发言人批驳赖清德所谓讲话')).toBe('国防部新闻发言人批驳赖清德所谓讲话')
  })
})

describe('activitySegments', () => {
  const thought = (key: string): ActivityItem => ({ kind: 'reasoning', text: 'hmm', key })
  const said = (key: string): ActivityItem => ({ kind: 'note', text: 'Let me look.', key })
  const step = (key: string): ActivityItem => ({ kind: 'tool', key, status: 'succeeded', call: { type: 'tool_call', id: key, name: 'web_search', rawArguments: '{}' } })

  it('folds the thinking and steps between what the model said, keeping its words as they are', () => {
    const segments = activitySegments([thought('a'), said('b'), step('c'), thought('d'), step('e')])
    expect(segments?.map((s) => (s.kind === 'note' ? s.item.key : s.items.map((i) => i.key).join('')))).toEqual(['a', 'b', 'cde'])
  })

  it('has no parts when the model said nothing on its way', () => {
    expect(activitySegments([thought('a'), step('b')])).toBeNull()
  })
})

describe('stepLabel for skills', () => {
  // The key and its values, so the test reads what was asked for.
  const t = ((key: string, values?: Record<string, unknown>) => `${key}${values ? ' ' + JSON.stringify(values) : ''}`) as Parameters<typeof stepLabel>[0]
  const skillStep = (args: Record<string, unknown>, done = true, details?: Record<string, unknown>) =>
    ({
      kind: 'tool',
      key: 'k',
      status: done ? 'succeeded' : 'running',
      call: { type: 'tool_call', id: 'k', name: 'skill', rawArguments: JSON.stringify(args) },
      result: done ? { type: 'tool_result', callId: 'k', status: 'succeeded', content: '', details } : undefined,
    }) as Parameters<typeof stepLabel>[1]

  it('names the skill opened, and the file read', () => {
    expect(stepLabel(t, skillStep({ op: 'view', name: 'house-style' }), false)).toBe('chat.tool.skill-open {"name":"house-style"}')
    expect(stepLabel(t, skillStep({ op: 'view', name: 'house-style' }, false), true)).toBe('chat.tool.skill-opening {"name":"house-style"}')
    expect(stepLabel(t, skillStep({ op: 'read', name: 'pdf', path: './a.md' }, true, { name: 'pdf', path: 'a.md' }), false)).toBe(
      'chat.tool.skill-read {"name":"pdf","path":"a.md"}',
    )
  })

  it('says only that a skill was opened when it has no name yet', () => {
    expect(stepLabel(t, skillStep({}, false), true)).toBe('chat.tool.skill-opening-generic')
  })
})

describe('activityHead with skills', () => {
  const t = ((key: string, values?: Record<string, unknown>) => `${key}${values ? ' ' + JSON.stringify(values) : ''}`) as Parameters<typeof activityHead>[0]
  const call = (key: string, args: Record<string, unknown>): ActivityItem => ({
    kind: 'tool',
    key,
    status: 'succeeded',
    call: { type: 'tool_call', id: key, name: 'skill', rawArguments: JSON.stringify(args) },
  })

  it('counts skills, not the calls that read them', () => {
    const head = activityHead(t, [call('a', { op: 'view', name: 'pdf' }), call('b', { op: 'read', name: 'pdf', path: 'x.md' })], false)
    expect(head.title).toBe('Chat.tool.skills {"count":1}')
  })
})
