import { describe, expect, it, vi } from 'vitest'

import { isChunkLoadError, noteChunkError, onPieceMissing, seeBuild, type BuildWatch } from './build-check'

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

describe('a piece of the app that could not be fetched', () => {
  it('is recognised in each browser\'s words', () => {
    for (const message of [
      'Failed to fetch dynamically imported module: https://chat.example/assets/PdfView-Cq1QYfcV.js',
      'error loading dynamically imported module: https://chat.example/assets/PdfView-Cq1QYfcV.js',
      'Importing a module script failed.',
      'Unable to preload CSS for /assets/WordView-abc.css',
    ]) {
      expect(isChunkLoadError(new TypeError(message))).toBe(true)
    }
    expect(isChunkLoadError('Failed to fetch dynamically imported module: x')).toBe(true)
  })

  it('is not any other failure', () => {
    for (const error of [new TypeError('Failed to fetch'), new Error("Cannot read properties of undefined (reading 'x')"), null, undefined, { message: 'x' }]) {
      expect(isChunkLoadError(error)).toBe(false)
    }
  })

  it('tells the reader once, and only for that', () => {
    const tell = vi.fn()
    onPieceMissing(tell)
    expect(noteChunkError(new Error('boom'))).toBe(false)
    expect(tell).not.toHaveBeenCalled()
    expect(noteChunkError(new TypeError('Importing a module script failed.'))).toBe(true)
    expect(noteChunkError(new TypeError('Importing a module script failed.'))).toBe(true)
    expect(tell).toHaveBeenCalledTimes(1)
  })
})
