import { describe, expect, it } from 'vitest'

import { quotePreview } from './quote-preview'

describe('quotePreview', () => {
  it('reads the Markdown as text, a line a part', () => {
    expect(quotePreview('## 能做什么\n\n### 无需认证\n- **看**最新帖')).toBe('能做什么 · 无需认证 · 看最新帖')
  })

  it('keeps a plain passage as it is', () => {
    expect(quotePreview('能做什么：总标题，说明这个 MCP 能做什么。')).toBe('能做什么：总标题，说明这个 MCP 能做什么。')
  })
})
