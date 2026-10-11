import { describe, expect, it } from 'vitest'

import { DEFAULT_BLOCKED_CATEGORIES, MODERATION_CATEGORIES, categorySlug, moderationSchema } from './moderation-schema'

const messages = {
  required: 'required',
  whole: 'whole',
  range: (min: number, max: number) => `${min}-${max}`,
  score: 'score',
  url: 'url',
  keyRequired: 'key',
  modelRequired: 'model',
}

const values = (patch: Record<string, unknown> = {}) => ({
  mode: 'off',
  openaiApiKey: '',
  openaiBaseUrl: '',
  model: '',
  categories: ['violence/graphic'],
  minScore: 0,
  exemptAdmins: false,
  onError: 'allow',
  strikes: 3,
  strikeWindowHours: 24,
  guestBanHours: 168,
  ...patch,
})

const parse = (patch: Record<string, unknown> = {}, context = { keyStored: false, clearingKey: false }) =>
  moderationSchema(messages, context).safeParse(values(patch))

const issues = (result: ReturnType<typeof parse>) =>
  result.success ? [] : result.error.issues.map((issue) => `${issue.path.join('.')}:${issue.message}`)

describe('the moderation form', () => {
  it('accepts the stored settings and reads typed numbers', () => {
    expect(parse().success).toBe(true)
    expect(parse({ strikes: '5', strikeWindowHours: '48', guestBanHours: '0', minScore: '0.4' })).toMatchObject({
      success: true,
      data: { strikes: 5, strikeWindowHours: 48, guestBanHours: 0, minScore: 0.4 },
    })
  })

  it('does not read an empty box as zero, which would switch a sanction off', () => {
    expect(issues(parse({ strikes: '' }))).toEqual(['strikes:required'])
    expect(issues(parse({ guestBanHours: '  ' }))).toEqual(['guestBanHours:required'])
    expect(issues(parse({ minScore: '' }))).toEqual(['minScore:required'])
  })

  it('holds each number to the server ranges', () => {
    expect(issues(parse({ strikes: '1.5' }))).toEqual(['strikes:whole'])
    expect(issues(parse({ strikes: '-1' }))).toEqual(['strikes:0-1000'])
    expect(issues(parse({ strikeWindowHours: '0' }))).toEqual(['strikeWindowHours:1-8760'])
    expect(issues(parse({ guestBanHours: '999999' }))).toEqual(['guestBanHours:0-87600'])
    expect(issues(parse({ minScore: '1.5' }))).toEqual(['minScore:score'])
    expect(issues(parse({ minScore: 'high' }))).toEqual(['minScore:score'])
  })

  it('checks the base URL', () => {
    expect(parse({ openaiBaseUrl: 'https://relay.example.com/v1' }).success).toBe(true)
    expect(issues(parse({ openaiBaseUrl: 'relay.example.com' }))).toEqual(['openaiBaseUrl:url'])
    expect(issues(parse({ openaiBaseUrl: 'ftp://x' }))).toEqual(['openaiBaseUrl:url'])
  })

  it('wants a key for the OpenAI mode, typed or already stored and not being cleared', () => {
    expect(issues(parse({ mode: 'openai' }))).toEqual(['openaiApiKey:key'])
    expect(parse({ mode: 'openai', openaiApiKey: 'sk-1234' }).success).toBe(true)
    expect(parse({ mode: 'openai' }, { keyStored: true, clearingKey: false }).success).toBe(true)
    expect(issues(parse({ mode: 'openai' }, { keyStored: true, clearingKey: true }))).toEqual(['openaiApiKey:key'])
    expect(parse({ mode: 'openai', openaiApiKey: 'sk-new' }, { keyStored: true, clearingKey: true }).success).toBe(true)
  })

  it('wants a model for the model mode', () => {
    expect(issues(parse({ mode: 'model' }))).toEqual(['model:model'])
    expect(parse({ mode: 'model', model: 'gpt-x' }).success).toBe(true)
    // Off needs neither.
    expect(parse({ mode: 'off' }).success).toBe(true)
  })

  it('knows only the categories the server does', () => {
    expect(parse({ categories: ['nonsense'] }).success).toBe(false)
    expect(MODERATION_CATEGORIES).toHaveLength(13)
    expect(DEFAULT_BLOCKED_CATEGORIES.every((c) => MODERATION_CATEGORIES.includes(c))).toBe(true)
    expect(categorySlug('self-harm/instructions')).toBe('self-harm-instructions')
    expect(MODERATION_CATEGORIES.map(categorySlug).every((slug) => !slug.includes('/'))).toBe(true)
  })
})
