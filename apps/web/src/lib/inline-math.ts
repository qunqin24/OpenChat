/**
 * Single-dollar math that is unmistakably math, written the way the page reads
 * it: `$\alpha$` becomes `$$\alpha$$`.
 *
 * The renderer only takes `$$…$$` (markdown-components.ts): answers quote
 * prices all the time, and with single-dollar math on, "$999 or $1,099" turns
 * the words between the two prices into a formula. The system prompt asks for
 * double dollars, and models still write `$\alpha$` out of habit, which then
 * shows as its source. So a pair is taken as math only when it is both:
 *
 *  - shaped like math (Pandoc's rule): the opening `$` is followed by a
 *    non-space, the closing one preceded by a non-space and not followed by a
 *    digit, and neither is part of `$$` or escaped; and
 *  - TeX inside: a command (`\alpha`), `^`, `_` or braces - or a lone letter
 *    such as `$n$`.
 *
 * A price is neither: "$5 and $10" fails the first (a space before the second
 * `$`, a digit after it), "$5-$10" the second. Code is left alone: fenced
 * blocks and inline code spans.
 */
export function promoteInlineMath(text: string): string {
  if (!text.includes('$')) return text
  let fenced = false
  return text
    .split('\n')
    .map((line) => {
      if (/^\s*(```|~~~)/.test(line)) {
        fenced = !fenced
        return line
      }
      if (fenced || !line.includes('$')) return line
      // Inline code spans keep their dollars: only the parts between them change.
      return line
        .split(/(`+[^`]*`+)/)
        .map((part, i) => (i % 2 === 1 ? part : promoteLine(part)))
        .join('')
    })
    .join('\n')
}

const pair = /(^|[^\\$\w])\$(?!\$)(?=\S)([^$\n]*?[^\s\\])\$(?![$\d])/g

function promoteLine(line: string): string {
  return line.replace(pair, (whole, before: string, tex: string) =>
    looksLikeTex(tex) ? `${before}$$${tex}$$` : whole,
  )
}

function looksLikeTex(tex: string): boolean {
  return /\\[A-Za-z]+|[\^_{}]/.test(tex) || /^[A-Za-z]$/.test(tex)
}
