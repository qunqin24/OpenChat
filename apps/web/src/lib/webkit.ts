/**
 * Whether the page runs on WebKit: Safari, and every browser on iOS (they all
 * have to use it). Some CSS is left out there (`data-webkit` on <html>, set in
 * main.tsx). Open WebUI's test (Message.svelte): the vendor catches iOS web
 * views and home-screen apps, whose user agent carries no "Safari"; the user
 * agent catches the rest, as long as it is not Chrome or Android claiming
 * Safari too.
 */
export function isWebKit(nav: Pick<Navigator, 'vendor' | 'userAgent'> = navigator): boolean {
  return nav.vendor === 'Apple Computer, Inc.' || /^((?!chrome|android).)*safari/i.test(nav.userAgent)
}
