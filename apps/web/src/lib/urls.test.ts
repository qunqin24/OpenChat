import { describe, expect, it } from 'vitest'

import { isHttpUrl, siteName } from './urls'

describe('isHttpUrl', () => {
  it('accepts http and https', () => {
    expect(isHttpUrl('https://example.com/a?b=1')).toBe(true)
    expect(isHttpUrl('http://example.com')).toBe(true)
    expect(isHttpUrl('HTTPS://EXAMPLE.COM')).toBe(true)
  })

  it('refuses everything else that could end up in an href', () => {
    for (const url of [
      'javascript:alert(1)',
      ' javascript:alert(1)',
      'JaVaScRiPt:alert(1)',
      'data:text/html,<script>alert(1)</script>',
      'vbscript:x',
      'file:///etc/passwd',
      'ftp://example.com',
      '//example.com',
      '/relative',
      'example.com',
      '',
    ]) {
      expect(isHttpUrl(url), url).toBe(false)
    }
    expect(isHttpUrl(undefined)).toBe(false)
    expect(isHttpUrl(42)).toBe(false)
  })
})

describe('siteName', () => {
  it('names the host without www', () => {
    expect(siteName('https://www.example.com/x')).toBe('example.com')
  })
})
