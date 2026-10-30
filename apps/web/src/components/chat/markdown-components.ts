import { cjk } from '@streamdown/cjk'
import { useEffect, useSyncExternalStore } from 'react'
import type { Components, LinkSafetyConfig, PluginConfig } from 'streamdown'

import { promoteInlineMath } from '@openchat/chat-core/inline-math'
import { HtmlPreview } from './HtmlPreview'
import { MarkdownLink } from './MarkdownLink'
import { MarkdownTable } from './MarkdownTable'
import { MermaidDiagram } from './MermaidDiagram'

/** Streamdown overrides shared by every Markdown surface in the chat. */
export const markdownComponents: Components = { table: MarkdownTable, a: MarkdownLink }

/**
 * Streamdown plugins shared by every Markdown surface in the chat.
 *
 * `cjk` because CommonMark's emphasis rules were written for languages that
 * put spaces between words: a closing `**` right after full-width punctuation
 * and right before a Han character — `**通货膨胀（Inflation）**是…` — does not
 * count as closing, so the asterisks show and nothing is bold. Models write
 * Chinese exactly like that all the time.
 *
 * `code` highlights code blocks (Shiki) and `math` renders `$$…$$` with KaTeX,
 * its stylesheet and fonts bundled rather than fetched from a CDN. Single-dollar
 * inline math stays off: answers quote prices in dollars all the time, and
 * "$999 or $1,099" would turn the words between them into a formula. A pair
 * that is plainly TeX - `$\alpha$` - is rewritten to double dollars instead
 * (prepareMarkdown, lib/inline-math.ts).
 *
 * Those two are most of the Markdown code by weight — KaTeX alone is more than
 * the rest of the chat page — and most answers need neither, so they are
 * fetched the first time an answer has a code block or a formula, and every
 * Markdown on the page picks them up then. Until they arrive a code block is
 * plain and a formula shows as its source, for the moment it takes.
 */
// html blocks can be previewed as the page they describe (HtmlPreview), and
// mermaid blocks are drawn as diagrams (MermaidDiagram, which fetches Mermaid
// itself for the first one).
let plugins: PluginConfig = {
  cjk,
  renderers: [
    { language: ['html', 'htm'], component: HtmlPreview },
    { language: 'mermaid', component: MermaidDiagram },
  ],
}
const listeners = new Set<() => void>()
const loading = { code: false, math: false }

function add(extra: PluginConfig) {
  plugins = { ...plugins, ...extra }
  listeners.forEach((listener) => listener())
}

function ensure(text: string) {
  if (!loading.code && /```|~~~/.test(text)) {
    loading.code = true
    void import('@streamdown/code').then(({ code }) => add({ code }))
  }
  if (!loading.math && (text.includes('$$') || promoteInlineMath(text) !== text)) {
    loading.math = true
    void Promise.all([import('@streamdown/math'), import('katex/dist/katex.min.css')]).then(([{ createMathPlugin }]) =>
      add({ math: createMathPlugin() }),
    )
  }
}

/** The plugins for this text: the ones loaded so far, loading what it needs. */
export function useMarkdownPlugins(text: string): PluginConfig {
  useEffect(() => ensure(text), [text])
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    () => plugins,
  )
}

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
 * fences are left alone. Single-dollar TeX is made double first.
 */
export function prepareMarkdown(text: string): string {
  text = promoteInlineMath(text)
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
