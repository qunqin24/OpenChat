import { useRef } from 'react'
import { Streamdown } from 'streamdown'

import { useArrivalFade } from './arrival-fade'
import { markdownComponents, markdownLinkSafety, prepareMarkdown, useMarkdownPlugins } from './markdown-components'

/**
 * Markdown as the chat draws it: an answer's text, the reasoning, a
 * conversation's summary. Text is shown as it arrives, each batch fading in
 * together (`useArrivalFade`); `fresh`: the part began while the reader
 * watched, so even its first batch fades in.
 */
export function Markdown({ text, streaming = false, fresh = false }: { text: string; streaming?: boolean; fresh?: boolean }) {
  const plugins = useMarkdownPlugins(text)
  const content = useRef<HTMLDivElement>(null)
  const layer = useRef<HTMLSpanElement>(null)
  useArrivalFade(content, layer, text, fresh)
  return (
    <div className="n-markdown">
      {/* The local typography layer styles the actual Streamdown output;
          markdown-body keeps the ported table selectors working. */}
      <div className="markdown-body" ref={content}>
        <Streamdown className="n-prose" parseIncompleteMarkdown={streaming} isAnimating={streaming} components={markdownComponents} plugins={plugins} linkSafety={markdownLinkSafety}>
          {prepareMarkdown(text)}
        </Streamdown>
      </div>
      <span ref={layer} className="n-arrival" aria-hidden="true" />
    </div>
  )
}
