import { splitSegments } from '@openchat/chat-core/genui/segments'

import GenUIBlock from './genui/GenUIBlock'
import { Markdown } from './MarkdownText'

/**
 * What the model said: Markdown, with any `openchat-ui` blocks in it drawn as
 * components where they sit - including one still being written at the end of
 * a stream. The answer, and the words it said on its way, inside the process
 * (ToolActivity): either may hold an interface block. `fresh`: it began while
 * the reader watched (`Markdown`).
 */
export function AnswerText({ text, streaming, fresh = false }: { text: string; streaming: boolean; fresh?: boolean }) {
  const segments = splitSegments(text, streaming)
  return segments.map((segment, i) =>
    segment.kind === 'ui' ? (
      <GenUIBlock key={i} source={segment.source} complete={segment.complete} />
    ) : (
      <Markdown key={i} text={segment.text} streaming={streaming && i === segments.length - 1} fresh={fresh} />
    ),
  )
}
