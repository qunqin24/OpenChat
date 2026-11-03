import { plainText } from './outline'

/**
 * A quoted passage as its card shows it (QuoteBlock): its Markdown read as
 * text, line by line, the lines joined by a dot - a writing block's headings
 * "## 能做什么", "### 无需认证" show as "能做什么 · 无需认证", not as marks.
 */
export function quotePreview(passage: string): string {
  return passage
    .split('\n')
    .map((line) => plainText(line))
    .filter(Boolean)
    .join(' · ')
}
