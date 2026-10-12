import { describe, expect, it } from 'vitest'

import { seeBuild, type BuildWatch } from './build-check'

const fresh: BuildWatch = { loaded: null, told: false }

describe('seeBuild', () => {
  it('takes the first build seen as the one the page was loaded from', () => {
    const { watch, tell } = seeBuild(fresh, 'aaa')
    expect(watch.loaded).toBe('aaa')
    expect(tell).toBe(false)
  })

  it('tells once when the server names another build, and not again', () => {
    let watch: BuildWatch = { loaded: 'aaa', told: false }
    expect(seeBuild(watch, 'aaa').tell).toBe(false)
    const changed = seeBuild(watch, 'bbb')
    expect(changed.tell).toBe(true)
    watch = changed.watch
    expect(seeBuild(watch, 'bbb').tell).toBe(false)
    expect(seeBuild(watch, 'ccc').tell).toBe(false)
  })

  it('ignores responses without a build (development, a proxy that drops it)', () => {
    expect(seeBuild(fresh, null)).toEqual({ watch: fresh, tell: false })
    expect(seeBuild({ loaded: 'aaa', told: false }, null).tell).toBe(false)
  })
})
