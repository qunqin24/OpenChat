import { describe, expect, it } from 'vitest'

import { compareVersions, isNewer, isVersion } from './semver'

describe('isNewer', () => {
  // The cases of the server's TestNewerVersion (version_test.go).
  it.each([
    ['1.0.1', '1.0.0', true],
    ['1.1.0', '1.0.9', true],
    ['2.0.0', '1.99.99', true],
    ['1.0.0', '1.0.0', false],
    ['1.0.0', '1.0.1', false],
    ['1.0.0', '1.0.0-rc.1', true],
    ['1.0.0-rc.2', '1.0.0-rc.1', true],
    ['1.0.0-rc.10', '1.0.0-rc.9', true],
    ['1.0.0-rc.1', '1.0.0', false],
    ['1.0.1', 'dev', false],
    ['nonsense', '1.0.0', false],
  ])('%s over %s is %s', (candidate, current, want) => {
    expect(isNewer(candidate, current)).toBe(want)
  })

  it('reads release tags with their v', () => {
    expect(isNewer('v1.4.0', '1.3.0')).toBe(true)
    expect(isNewer('v1.4.0-rc.1', '1.4.0')).toBe(false)
    expect(isNewer('v1.4.0', 'v1.4.0')).toBe(false)
  })
})

describe('compareVersions', () => {
  it('orders both ways', () => {
    expect(compareVersions('1.0.0', '1.0.1')).toBe(-1)
    expect(compareVersions('1.0.1', '1.0.0')).toBe(1)
    expect(compareVersions('1.0.0', '1.0.0')).toBe(0)
    expect(compareVersions('1.0.0-rc.1', '1.0.0')).toBe(-1)
  })

  it('orders pre-release identifiers: numbers below words, shorter first', () => {
    expect(compareVersions('1.0.0-1', '1.0.0-alpha')).toBe(-1)
    expect(compareVersions('1.0.0-alpha', '1.0.0-beta')).toBe(-1)
    expect(compareVersions('1.0.0-rc', '1.0.0-rc.1')).toBe(-1)
  })

  it('ignores build metadata', () => {
    expect(compareVersions('1.0.0+abc', '1.0.0')).toBe(0)
    expect(compareVersions('1.0.0-rc.1+abc', '1.0.0-rc.1')).toBe(0)
  })

  it('has no order for what is not a version', () => {
    expect(compareVersions('dev', '1.0.0')).toBeNull()
    expect(compareVersions('1.0.0', 'main-abc123')).toBeNull()
    expect(compareVersions('1.0', '1.0.0')).toBeNull()
    expect(compareVersions('1.01.0', '1.0.0')).toBeNull()
    expect(compareVersions('', '')).toBeNull()
  })
})

describe('isVersion', () => {
  it('accepts versions with or without a v', () => {
    expect(isVersion('1.4.0')).toBe(true)
    expect(isVersion('v1.4.0-rc.1')).toBe(true)
    expect(isVersion('latest-build')).toBe(false)
  })
})
