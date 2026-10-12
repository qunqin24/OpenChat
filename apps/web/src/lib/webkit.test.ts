import { describe, expect, it } from 'vitest'

import { isWebKit } from './webkit'

describe('isWebKit', () => {
  it('knows Safari on the Mac and on the iPhone', () => {
    expect(
      isWebKit({
        vendor: 'Apple Computer, Inc.',
        userAgent:
          'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15',
      }),
    ).toBe(true)
    expect(
      isWebKit({
        vendor: '',
        userAgent:
          'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1',
      }),
    ).toBe(true)
  })

  it('knows a home-screen app by the vendor alone', () => {
    expect(
      isWebKit({
        vendor: 'Apple Computer, Inc.',
        userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148',
      }),
    ).toBe(true)
  })

  it('is not fooled by Chrome or Android naming Safari', () => {
    expect(
      isWebKit({
        vendor: 'Google Inc.',
        userAgent:
          'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36',
      }),
    ).toBe(false)
    expect(
      isWebKit({
        vendor: 'Google Inc.',
        userAgent:
          'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36',
      }),
    ).toBe(false)
    expect(isWebKit({ vendor: '', userAgent: 'Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0' })).toBe(false)
  })
})
