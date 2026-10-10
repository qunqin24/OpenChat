import { cjk } from '@streamdown/cjk'
import { code } from '@streamdown/code'
import { createMathPlugin } from '@streamdown/math'
import type { Components, LinkSafetyConfig, PluginConfig } from 'streamdown'

import { MarkdownTable } from './MarkdownTable'

/** Streamdown overrides shared by every Markdown surface in the chat. */
export const markdownComponents: Components = { table: MarkdownTable }

/**
 * Streamdown plugins shared by every Markdown surface in the chat.
 *
 * `cjk` because CommonMark's emphasis rules were written for languages that
 * put spaces between words: a closing `**` right after full-width punctuation
 * and right before a Han character — `**通货膨胀（Inflation）**是…` — does not
 * count as closing, so the asterisks show and nothing is bold. Models write
 * Chinese exactly like that all the time.
 *
 * `code` highlights code blocks (Shiki, loaded per language on first use).
 *
 * `math` renders `$$…$$` with KaTeX; its stylesheet and fonts are bundled
 * (main.tsx), not fetched from a CDN. Single-dollar inline math stays off:
 * answers quote prices in dollars all the time, and "$999 or $1,099" would
 * turn the words between them into a formula.
 */
export const markdownPlugins: PluginConfig = { cjk, code, math: createMathPlugin() }

/**
 * Links open directly in a new tab. Streamdown's own guard puts every link
 * behind an English "Open external link?" dialog drawn outside both design
 * systems — a confirmation on every source a model cites, which is most links
 * in an answer.
 */
export const markdownLinkSafety: LinkSafetyConfig = { enabled: false }

/**
 * A formula alone on its line is a display formula. Models write
 * `$$x = \frac{-b}{2a}$$` on one line, and to Markdown that is inline math —
 * set small, its fractions shrunk to fit the line — because display math needs
 * the dollars on lines of their own. This moves them there. Lines inside code
 * fences are left alone.
 */
export function prepareMarkdown(text: string): string {
  if (!text.includes('$$')) return text
  let fenced = false
  return text
    .split('\n')
    .map((line) => {
      if (/^\s*(```|~~~)/.test(line)) fenced = !fenced
      if (fenced) return line
      const match = /^(\s*)\$\$([^$]+)\$\$\s*$/.exec(line)
      return match ? `${match[1]}$$\n${match[1]}${match[2]}\n${match[1]}$$` : line
    })
    .join('\n')
}
