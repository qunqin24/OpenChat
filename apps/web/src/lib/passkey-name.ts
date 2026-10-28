/**
 * A name for a passkey being added, from the browser it is made in: the
 * browser and the system, such as "Chrome · macOS", so a list of passkeys says
 * where each came from. The person can rename it.
 */
export function passkeyName(userAgent: string): string {
  const browser =
    /Edg\//.test(userAgent) ? 'Edge'
    : /OPR\//.test(userAgent) ? 'Opera'
    : /Firefox\//.test(userAgent) ? 'Firefox'
    : /Chrome\//.test(userAgent) ? 'Chrome'
    : /Safari\//.test(userAgent) ? 'Safari'
    : ''
  const system =
    /iPhone|iPad|iPod/.test(userAgent) ? 'iOS'
    : /Android/.test(userAgent) ? 'Android'
    : /Mac OS X|Macintosh/.test(userAgent) ? 'macOS'
    : /Windows/.test(userAgent) ? 'Windows'
    : /CrOS/.test(userAgent) ? 'ChromeOS'
    : /Linux/.test(userAgent) ? 'Linux'
    : ''
  return [browser, system].filter(Boolean).join(' · ')
}
