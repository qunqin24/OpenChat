/** A URL's site, as a person would name it: the host without `www.`. */
export function siteName(url: string): string {
  try {
    return new URL(url).host.replace(/^www\./, '')
  } catch {
    return url
  }
}
