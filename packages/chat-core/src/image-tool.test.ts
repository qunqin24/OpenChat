import { describe, expect, it } from 'vitest'

import { imageRequestOf, pictureSrc, picturesOf, placeholderSize } from './image-tool'

describe('imageRequestOf', () => {
  it('reads the arguments, with defaults for what is missing or wrong', () => {
    expect(imageRequestOf({ arguments: { prompt: ' a lake ', aspect: 'landscape', count: 2 } })).toEqual({ prompt: 'a lake', aspect: '3:2', count: 2 })
    expect(imageRequestOf({ arguments: { prompt: 'x', aspect: '16:9', size: '1920x1080' } })).toEqual({ prompt: 'x', aspect: '16:9', size: { width: 1920, height: 1080 }, count: 1 })
    expect(imageRequestOf({ arguments: { prompt: 'x', size: '99999x1' } })?.size).toBeUndefined()
    expect(imageRequestOf({ arguments: { prompt: 'x', aspect: 'wide', count: 99 } })).toEqual({ prompt: 'x', aspect: 'auto', count: 10 })
    expect(imageRequestOf({ arguments: {} })).toEqual({ prompt: '', aspect: 'auto', count: 1 })
  })
})

describe('picturesOf', () => {
  const picture = { id: 'a1', handle: 'img_a1', mediaType: 'image/png', width: 1024, height: 768 }
  it('takes the pictures of a call that succeeded', () => {
    expect(picturesOf({ status: 'succeeded', details: { images: [picture, { id: 3 }] } })).toEqual([picture])
  })
  it('has none for a call that failed or is still going', () => {
    expect(picturesOf({ status: 'failed', details: { images: [picture] } })).toEqual([])
    expect(picturesOf(undefined)).toEqual([])
  })
})

describe('placeholderSize and pictureSrc', () => {
  it('sizes the placeholder by its shape', () => {
    expect(placeholderSize({ aspect: '2:3' })).toEqual({ width: 1024, height: 1536 })
    expect(placeholderSize({ aspect: '16:9' })).toEqual({ width: 1536, height: 864 })
    expect(placeholderSize({ aspect: '1:1' })).toEqual({ width: 1024, height: 1024 })
    expect(placeholderSize({ aspect: 'auto' })).toBeNull()
    expect(placeholderSize({ aspect: '16:9', size: { width: 3840, height: 2160 } })).toEqual({ width: 3840, height: 2160 })
  })
  it('points at the view redirect', () => {
    expect(pictureSrc('/api/v1', 'a b')).toBe('/api/v1/files/a%20b/view')
  })
})
