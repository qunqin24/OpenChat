import { readFileSync } from 'node:fs'

import { describe, expect, it } from 'vitest'

/**
 * The two locale files, checked as text as well as as data.
 *
 * JSON.parse keeps the last of two equal keys in one object and says nothing,
 * so a key written twice silently shows the other string: the user bulk
 * action's error once read "{{n}} failed" because the channels' message had
 * the same key further down. The type check of `t()` cannot see that either.
 */
const LOCALES = ['zh', 'en'] as const

const source = (locale: string) => readFileSync(new URL(`./${locale}/common.json`, import.meta.url), 'utf8')

/** Every key that appears twice in the same object, as its dotted path. */
function duplicateKeys(text: string): string[] {
  const duplicates: string[] = []
  // One frame per open object: its path and the keys seen in it. Arrays get a
  // frame too, so their elements' keys are not counted against the parent.
  const stack: { path: string; keys: Set<string> | null }[] = []
  let lastKey = ''
  let i = 0
  while (i < text.length) {
    const c = text[i]
    if (c === '"') {
      let j = i + 1
      while (text[j] !== '"') j += text[j] === '\\' ? 2 : 1
      const value = JSON.parse(text.slice(i, j + 1)) as string
      let k = j + 1
      while (/\s/.test(text[k] ?? '')) k++
      const frame = stack[stack.length - 1]
      if (text[k] === ':' && frame?.keys) {
        const path = frame.path ? `${frame.path}.${value}` : value
        if (frame.keys.has(value)) duplicates.push(path)
        frame.keys.add(value)
        lastKey = path
      }
      i = j + 1
      continue
    }
    if (c === '{') stack.push({ path: stack.length === 0 ? '' : lastKey, keys: new Set() })
    if (c === '[') stack.push({ path: lastKey, keys: null })
    if (c === '}' || c === ']') stack.pop()
    i++
  }
  return duplicates
}

function flatten(value: unknown, prefix = '', out: Record<string, string> = {}): Record<string, string> {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    for (const [key, child] of Object.entries(value)) flatten(child, prefix ? `${prefix}.${key}` : key, out)
  } else {
    out[prefix] = String(value)
  }
  return out
}

const placeholders = (text: string) => new Set([...text.matchAll(/{{\s*(\w+)\s*}}/g)].map((m) => m[1]))

describe('the locale files', () => {
  it('finds a duplicate key, so the check below means something', () => {
    expect(duplicateKeys('{"a": {"b": 1, "c": [{"b": 2}], "b": 3}, "d": "x"}')).toEqual(['a.b'])
  })

  for (const locale of LOCALES) {
    it(`${locale}: no key is written twice in one object`, () => {
      expect(duplicateKeys(source(locale))).toEqual([])
    })
  }

  const zh = flatten(JSON.parse(source('zh')))
  const en = flatten(JSON.parse(source('en')))

  it('zh and en have the same keys', () => {
    expect(Object.keys(zh).filter((key) => !(key in en))).toEqual([])
    expect(Object.keys(en).filter((key) => !(key in zh))).toEqual([])
  })

  it('zh and en use the same placeholders', () => {
    const mismatched = Object.keys(zh).filter((key) => {
      const a = placeholders(zh[key] ?? '')
      const b = placeholders(en[key] ?? '')
      // A plural form may leave the count out ("Searched once").
      if (/_(zero|one|two|few|many|other)$/.test(key)) {
        a.delete('count')
        b.delete('count')
      }
      return a.size !== b.size || [...a].some((name) => !b.has(name))
    })
    expect(mismatched).toEqual([])
  })
})
