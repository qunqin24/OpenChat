import { common, createLowlight } from 'lowlight'
import { useMemo, type ReactNode } from 'react'
import { Text, useColorScheme } from 'react-native'

// The web colours code with Shiki (through Streamdown), whose regular
// expressions need a WebAssembly engine or ones Hermes cannot run. The app uses
// highlight.js's grammars (lowlight), plain JavaScript expressions, in the
// colours of the same GitHub themes the web uses.
const lowlight = createLowlight(common)

/** Past this, a block is shown as it is: colouring it would hold up the screen. */
const MAX_CHARS = 20_000

type Palette = Record<string, string>

const light: Palette = {
  keyword: '#cf222e',
  'selector-tag': '#cf222e',
  type: '#953800',
  built_in: '#953800',
  'title.class_': '#953800',
  title: '#8250df',
  'title.function_': '#8250df',
  string: '#0a3069',
  regexp: '#0a3069',
  'selector-attr': '#0a3069',
  number: '#0550ae',
  literal: '#0550ae',
  attr: '#0550ae',
  attribute: '#0550ae',
  meta: '#0550ae',
  symbol: '#0550ae',
  variable: '#953800',
  'template-variable': '#953800',
  params: '#24292f',
  comment: '#6e7781',
  doc: '#6e7781',
  quote: '#6e7781',
  tag: '#116329',
  name: '#116329',
  'selector-class': '#116329',
  'selector-id': '#116329',
  section: '#0550ae',
  bullet: '#953800',
  addition: '#116329',
  deletion: '#82071e',
}

const dark: Palette = {
  keyword: '#ff7b72',
  'selector-tag': '#ff7b72',
  type: '#ffa657',
  built_in: '#ffa657',
  'title.class_': '#ffa657',
  title: '#d2a8ff',
  'title.function_': '#d2a8ff',
  string: '#a5d6ff',
  regexp: '#a5d6ff',
  'selector-attr': '#a5d6ff',
  number: '#79c0ff',
  literal: '#79c0ff',
  attr: '#79c0ff',
  attribute: '#79c0ff',
  meta: '#79c0ff',
  symbol: '#79c0ff',
  variable: '#ffa657',
  'template-variable': '#ffa657',
  params: '#c9d1d9',
  comment: '#8b949e',
  doc: '#8b949e',
  quote: '#8b949e',
  tag: '#7ee787',
  name: '#7ee787',
  'selector-class': '#7ee787',
  'selector-id': '#7ee787',
  section: '#79c0ff',
  bullet: '#ffa657',
  addition: '#aff5b4',
  deletion: '#ffa198',
}

interface HastNode {
  type: string
  value?: string
  tagName?: string
  properties?: { className?: string[] }
  children?: HastNode[]
}

/** The colour of a token from its hljs classes: the most specific one this palette knows. */
function colourOf(classes: string[] | undefined, palette: Palette): string | undefined {
  if (!classes) return undefined
  const names = classes.map((c) => c.replace(/^hljs-/, ''))
  // "title class_" is written as two classes; the joined form is the specific one.
  const joined = names.join('.')
  return palette[joined] ?? names.map((n) => palette[n]).find(Boolean)
}

function render(nodes: HastNode[] | undefined, palette: Palette, key = ''): ReactNode[] {
  return (nodes ?? []).map((node, i) => {
    if (node.type === 'text') return node.value ?? ''
    const colour = colourOf(node.properties?.className, palette)
    return (
      <Text key={`${key}${i}`} style={colour ? { color: colour } : undefined}>
        {render(node.children, palette, `${key}${i}.`)}
      </Text>
    )
  })
}

/**
 * The code, coloured for its language when that is one highlight.js knows
 * (by name or alias), as nested Text: so it selects and copies as plain text.
 * Anything else, or too long, is shown as it is.
 */
export function useHighlighted(code: string, lang: string | undefined): ReactNode {
  const palette = useColorScheme() === 'dark' ? dark : light
  return useMemo(() => {
    const language = lang?.toLowerCase().trim()
    if (!language || code.length > MAX_CHARS || !lowlight.registered(language)) return code
    try {
      return render(lowlight.highlight(language, code).children as HastNode[], palette)
    } catch {
      return code
    }
  }, [code, lang, palette])
}
