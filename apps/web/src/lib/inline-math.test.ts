import { describe, expect, it } from 'vitest'

import { promoteInlineMath } from './inline-math'

describe('promoteInlineMath', () => {
  it('turns single-dollar TeX into the double dollars the page renders', () => {
    expect(promoteInlineMath('$\\alpha$、$\\beta$ 是复数幅值')).toBe('$$\\alpha$$、$$\\beta$$ 是复数幅值')
    expect(promoteInlineMath('以 $|\\alpha|^2$ 的概率得到 0')).toBe('以 $$|\\alpha|^2$$ 的概率得到 0')
    expect(promoteInlineMath('$n$ 个量子比特表示 $2^n$ 个基态')).toBe('$$n$$ 个量子比特表示 $$2^n$$ 个基态')
    expect(promoteInlineMath('例如 $\\frac{1}{\\sqrt{2}}(|00\\rangle+|11\\rangle)$。')).toBe(
      '例如 $$\\frac{1}{\\sqrt{2}}(|00\\rangle+|11\\rangle)$$。',
    )
  })

  it('leaves prices alone', () => {
    for (const text of [
      'It costs $5 and $10 later.',
      '$999 or $1,099',
      'from $5-$10 a month',
      'US$5 and US$6',
      'a $5 fee',
    ]) {
      expect(promoteInlineMath(text)).toBe(text)
    }
  })

  it('leaves what is already double, escaped, or code alone', () => {
    for (const text of [
      'inline $$\\alpha$$ already',
      'an escaped \\$\\alpha$ dollar',
      'the variable `$x_1$` in code',
      '```\nlet a = $\\alpha$\n```',
    ]) {
      expect(promoteInlineMath(text)).toBe(text)
    }
  })

  it('does not reach across lines', () => {
    expect(promoteInlineMath('$\\alpha\nand more$')).toBe('$\\alpha\nand more$')
  })
})
