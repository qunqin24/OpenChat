import { describe, expect, it } from 'vitest'

import { resolveIcon } from './model-icons'

describe('resolveIcon', () => {
  it('draws a model by its family', () => {
    expect(resolveIcon(undefined, undefined, 'glm-5.3-flash', 'model')).toBe('zai-disc')
    expect(resolveIcon(undefined, undefined, 'claude-sonnet-5-5', 'model')).toBe('claude-color')
    expect(resolveIcon(undefined, undefined, 'qwen3.8-max', 'model')).toBe('qwen-color')
  })

  // The usage log has only the id a gateway serves, maker first.
  it('reads the family after a gateway id’s maker', () => {
    expect(resolveIcon(undefined, undefined, 'z-ai/glm-5.3-flash', 'model')).toBe('zai-disc')
    expect(resolveIcon(undefined, undefined, 'zai-org/GLM-5.3', 'model')).toBe('zai-disc')
    expect(resolveIcon(undefined, undefined, 'moonshotai/Kimi-K3', 'model')).toBe('kimi-color')
    expect(resolveIcon(undefined, undefined, 'deepseek/deepseek-v4-flash', 'model')).toBe('deepseek-color')
    expect(resolveIcon(undefined, undefined, 'Qwen/Qwen3.8-Flash', 'model')).toBe('qwen-color')
    expect(resolveIcon(undefined, undefined, 'MiniMaxAI/MiniMax-M3', 'model')).toBe('minimax-color')
  })

  it('draws a brand it is told before anything else', () => {
    expect(resolveIcon('commandcode', undefined, 'glm-5.3', 'vendor')).toBe('commandcode')
  })
})
