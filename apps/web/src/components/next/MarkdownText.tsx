import { Streamdown } from 'streamdown'

import { markdownComponents, markdownLinkSafety, prepareMarkdown, useMarkdownPlugins } from './markdown-components'

/** Markdown as the chat draws it: an answer's text, the reasoning, a conversation's summary. */
export function Markdown({ text, streaming = false }: { text: string; streaming?: boolean }) {
  const plugins = useMarkdownPlugins(text)
  return (
    <div className="n-markdown">
      {/* The local typography layer styles the actual Streamdown output;
          markdown-body keeps the ported table selectors working. */}
      <div className="markdown-body">
        <Streamdown className="n-prose" parseIncompleteMarkdown={streaming} components={markdownComponents} plugins={plugins} linkSafety={markdownLinkSafety}>
          {prepareMarkdown(text)}
        </Streamdown>
      </div>
    </div>
  )
}
