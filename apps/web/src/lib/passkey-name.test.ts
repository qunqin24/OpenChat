import { describe, expect, it } from 'vitest'

import { passkeyName } from './passkey-name'

describe('passkeyName', () => {
  it('names the browser and the system', () => {
    expect(passkeyName('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36')).toBe('Chrome · macOS')
    expect(passkeyName('Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1')).toBe('Safari · iOS')
    expect(passkeyName('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0')).toBe('Edge · Windows')
    expect(passkeyName('Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0')).toBe('Firefox · Linux')
  })

  it('is empty for what it cannot tell', () => {
    expect(passkeyName('curl/8.0')).toBe('')
  })
})
