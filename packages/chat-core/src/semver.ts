// Release versions as the app compares them: major.minor.patch with an
// optional pre-release (1.4.0, 1.4.0-rc.1), in tags with a leading v. This is
// the comparison of the server's apps/server/internal/api/version.go, kept to
// the same answers so the website's "update available" and the app's agree.

interface Version {
  core: [number, number, number]
  pre: string
}

function parse(input: string): Version | null {
  const v = input.startsWith('v') ? input.slice(1) : input
  const dash = v.indexOf('-')
  const head = dash < 0 ? v : v.slice(0, dash)
  const tail = dash < 0 ? '' : v.slice(dash + 1)
  // Build metadata does not order.
  const parts = head.split('+')[0]!.split('.')
  if (parts.length !== 3) return null
  const core: number[] = []
  for (const p of parts) {
    if (!/^\d+$/.test(p) || (p.length > 1 && p.startsWith('0'))) return null
    core.push(Number(p))
  }
  return { core: core as Version['core'], pre: tail.split('+')[0]! }
}

/** Whether `input` (1.4.0, v1.4.0-rc.1) is a version this file can order. */
export function isVersion(input: string): boolean {
  return parse(input) !== null
}

function compareNumbers(a: number, b: number): number {
  return a < b ? -1 : a > b ? 1 : 0
}

/**
 * Two pre-release strings the semver way: dot-separated identifiers, numbers
 * numerically and below words, and the one with more identifiers later.
 */
function comparePre(a: string, b: string): number {
  const as = a.split('.')
  const bs = b.split('.')
  for (let i = 0; i < as.length && i < bs.length; i++) {
    const x = as[i]!
    const y = bs[i]!
    const xNumber = /^\d+$/.test(x)
    const yNumber = /^\d+$/.test(y)
    if (xNumber && yNumber) {
      if (Number(x) !== Number(y)) return compareNumbers(Number(x), Number(y))
    } else if (xNumber) {
      return -1
    } else if (yNumber) {
      return 1
    } else if (x !== y) {
      return x < y ? -1 : 1
    }
  }
  return compareNumbers(as.length, bs.length)
}

/**
 * Orders two versions: -1 when `a` is older, 1 when newer, 0 when equal; null
 * when either is not a version (a development build's "0.0.0" is one, "dev"
 * is not), because those have no place among releases.
 */
export function compareVersions(a: string, b: string): -1 | 0 | 1 | null {
  const l = parse(a)
  const c = parse(b)
  if (!l || !c) return null
  for (let i = 0; i < 3; i++) {
    if (l.core[i] !== c.core[i]) return l.core[i]! > c.core[i]! ? 1 : -1
  }
  // Same core: a release is newer than its own pre-release.
  if (l.pre === c.pre) return 0
  if (l.pre === '') return 1
  if (c.pre === '') return -1
  return comparePre(l.pre, c.pre) > 0 ? 1 : -1
}

/** Whether `candidate` is a newer release than `current`; false when either is not a version. */
export function isNewer(candidate: string, current: string): boolean {
  return compareVersions(candidate, current) === 1
}
