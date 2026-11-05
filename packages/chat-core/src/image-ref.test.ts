import { describe, expect, it } from 'vitest'

import { imageRefMessage, parseImageRefs } from './image-ref'
import { parseSkills, skillMessage } from './skill-mention'

describe('image refs', () => {
  it('round-trips, after the skills', () => {
    const content = skillMessage(['draw'], imageRefMessage(['img_ab12cd34', 'img_ab12cd34', 'img_00ff00ff'], 'bluer, please'))
    expect(content).toBe('<skill name="draw"/>\n<image-ref id="img_ab12cd34"/>\n<image-ref id="img_00ff00ff"/>\nbluer, please')
    const { names, rest } = parseSkills(content)
    expect(names).toEqual(['draw'])
    expect(parseImageRefs(rest)).toEqual({ handles: ['img_ab12cd34', 'img_00ff00ff'], rest: 'bluer, please' })
  })
  it('leaves a message without them alone', () => {
    expect(parseImageRefs('<image-ref id="x"/>\nhi')).toEqual({ handles: [], rest: '<image-ref id="x"/>\nhi' })
    expect(imageRefMessage([], 'hi')).toBe('hi')
  })
})
