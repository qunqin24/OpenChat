/** A URL's site, as a person would name it: the host without `www.`. */
export function siteName(url: string): string {
  try {
    return new URL(url).host.replace(/^www\./, '')
  } catch {
    return url
  }
}

/**
 * Whether a URL is safe to put in a link: http or https. Addresses come out of
 * tool results - web pages the model read - so a `javascript:` or `data:` one
 * must never become an `href`.
 */
export function isHttpUrl(url: unknown): url is string {
  if (typeof url !== 'string') return false
  try {
    const { protocol } = new URL(url)
    return protocol === 'http:' || protocol === 'https:'
  } catch {
    return false
  }
}
