import { afterEach, describe, expect, it, vi } from 'vitest'

import { smoothScrollBehavior } from './motion'

afterEach(() => vi.unstubAllGlobals())

const reducing = (reduce: boolean) =>
  vi.stubGlobal('window', {
    matchMedia: (query: string) => ({ matches: reduce && query === '(prefers-reduced-motion: reduce)' }),
  })

describe('smoothScrollBehavior', () => {
  it('animates by default', () => {
    reducing(false)
    expect(smoothScrollBehavior()).toBe('smooth')
  })

  it('jumps when the reader asked for reduced motion', () => {
    reducing(true)
    expect(smoothScrollBehavior()).toBe('auto')
  })

  it('jumps where it cannot ask', () => {
    expect(smoothScrollBehavior()).toBe('auto')
  })
})
