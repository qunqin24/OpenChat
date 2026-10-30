import * as Clipboard from 'expo-clipboard'
import { Check, Copy } from 'lucide-react-native'
import { marked, type Token, type Tokens } from 'marked'
import { memo, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Linking, Pressable, ScrollView, StyleSheet, Text, View, type StyleProp, type TextStyle } from 'react-native'

import { promoteInlineMath } from '@openchat/chat-core/inline-math'
import { isHttpUrl } from '@openchat/chat-core/urls'

import { useReveal } from '@/hooks/reveal'
import { useColors, type Colors } from '@/theme'

const MONO = 'monospace'

/**
 * Markdown as the chat draws it: an answer's text, the reasoning. Revealed as
 * it streams (useReveal), then parsed with marked's lexer into blocks. Each
 * block is its own memoised element keyed by its source, so while an answer
 * streams only the last block - the one still growing - is drawn again.
 */
export function Markdown({ text, streaming = false, fresh = false }: { text: string; streaming?: boolean; fresh?: boolean }) {
  const shown = useReveal(text, streaming, fresh)
  const blocks = useMemo(() => marked.lexer(promoteInlineMath(shown), { gfm: true }), [shown])
  return (
    <View style={styles.root}>
      {blocks.map((token, index) => (
        <Block key={index} token={token} raw={token.raw} />
      ))}
    </View>
  )
}

/** Plain text revealed at the same pace, for what is not Markdown (notes in the activity line). */
export function PlainReveal({ text, streaming, style }: { text: string; streaming: boolean; style?: StyleProp<TextStyle> }) {
  const shown = useReveal(text, streaming)
  return <Text style={style}>{shown}</Text>
}

const Block = memo(
  function Block({ token }: { token: Token; raw: string }) {
    const colors = useColors()
    return <BlockView token={token} colors={colors} />
  },
  // A block with the same source draws the same; its token object is new each parse.
  (a, b) => a.raw === b.raw,
)

function BlockView({ token, colors }: { token: Token; colors: Colors }): ReactNode {
  const text = { color: colors.text }
  switch (token.type) {
    case 'space':
      return null
    case 'heading': {
      const t = token as Tokens.Heading
      return (
        <Text style={[styles.heading, headingSize[t.depth] ?? headingSize[6], text]} accessibilityRole="header">
          <Inline tokens={t.tokens} colors={colors} />
        </Text>
      )
    }
    case 'paragraph': {
      const t = token as Tokens.Paragraph
      return (
        <Text style={[styles.paragraph, text]} selectable>
          <Inline tokens={t.tokens} colors={colors} />
        </Text>
      )
    }
    case 'text': {
      const t = token as Tokens.Text
      return (
        <Text style={[styles.paragraph, text]} selectable>
          {t.tokens ? <Inline tokens={t.tokens} colors={colors} /> : t.text}
        </Text>
      )
    }
    case 'list': {
      const t = token as Tokens.List
      const start = typeof t.start === 'number' ? t.start : 1
      return (
        <View style={styles.list}>
          {t.items.map((item, i) => (
            <View key={i} style={styles.item}>
              <Text style={[styles.bullet, text]}>
                {item.task ? (item.checked ? '☑' : '☐') : t.ordered ? `${start + i}.` : '•'}
              </Text>
              <View style={styles.itemBody}>
                {item.tokens.map((child, j) => (
                  <BlockView key={j} token={child} colors={colors} />
                ))}
              </View>
            </View>
          ))}
        </View>
      )
    }
    case 'blockquote': {
      const t = token as Tokens.Blockquote
      return (
        <View style={[styles.quote, { borderLeftColor: colors.line }]}>
          {t.tokens.map((child, i) => (
            <BlockView key={i} token={child} colors={colors} />
          ))}
        </View>
      )
    }
    case 'code':
      return <CodeBlock code={(token as Tokens.Code).text} lang={(token as Tokens.Code).lang} colors={colors} />
    case 'table':
      return <Table token={token as Tokens.Table} colors={colors} />
    case 'hr':
      return <View style={[styles.hr, { backgroundColor: colors.line }]} />
    case 'html':
      return <Text style={[styles.paragraph, text]}>{(token as Tokens.HTML).text}</Text>
    default:
      return 'raw' in token && token.raw.trim() ? <Text style={[styles.paragraph, text]}>{token.raw}</Text> : null
  }
}

const headingSize: Record<number, TextStyle> = {
  1: { fontSize: 22, lineHeight: 30 },
  2: { fontSize: 19, lineHeight: 27 },
  3: { fontSize: 17, lineHeight: 25 },
  4: { fontSize: 16, lineHeight: 24 },
  5: { fontSize: 16, lineHeight: 24 },
  6: { fontSize: 16, lineHeight: 24 },
}

/** Inline runs: nested Text, so a paragraph wraps as one. */
function Inline({ tokens, colors }: { tokens?: Token[]; colors: Colors }): ReactNode {
  if (!tokens) return null
  return tokens.map((token, i) => <InlineToken key={i} token={token} colors={colors} />)
}

function InlineToken({ token, colors }: { token: Token; colors: Colors }): ReactNode {
  switch (token.type) {
    case 'text': {
      const t = token as Tokens.Text
      return t.tokens ? <Inline tokens={t.tokens} colors={colors} /> : t.text
    }
    case 'escape':
      return (token as Tokens.Escape).text
    case 'strong':
      return (
        <Text style={styles.strong}>
          <Inline tokens={(token as Tokens.Strong).tokens} colors={colors} />
        </Text>
      )
    case 'em':
      return (
        <Text style={styles.em}>
          <Inline tokens={(token as Tokens.Em).tokens} colors={colors} />
        </Text>
      )
    case 'del':
      return (
        <Text style={styles.del}>
          <Inline tokens={(token as Tokens.Del).tokens} colors={colors} />
        </Text>
      )
    case 'codespan':
      return <Text style={[styles.codespan, { backgroundColor: colors.bubble }]}>{(token as Tokens.Codespan).text}</Text>
    case 'br':
      return '\n'
    case 'link': {
      const t = token as Tokens.Link
      // Only web addresses open: a link is the model's output.
      const href = isHttpUrl(t.href) ? t.href : null
      return (
        <Text
          style={[styles.link, { color: colors.link, textDecorationColor: colors.text3 }]}
          onPress={href ? () => void Linking.openURL(href) : undefined}
        >
          <Inline tokens={t.tokens} colors={colors} />
        </Text>
      )
    }
    case 'image': {
      const t = token as Tokens.Image
      return t.text || t.href
    }
    default:
      return 'raw' in token ? token.raw : null
  }
}

function CodeBlock({ code, lang, colors }: { code: string; lang?: string; colors: Colors }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  return (
    <View style={[styles.code, { backgroundColor: colors.code, borderColor: colors.line }]}>
      <View style={styles.codeHead}>
        <Text style={[styles.codeLang, { color: colors.text3 }]}>{lang || 'text'}</Text>
        <Pressable
          hitSlop={10}
          accessibilityLabel={t('chat.copy')}
          onPress={() => {
            void Clipboard.setStringAsync(code).then(() => {
              setCopied(true)
              setTimeout(() => setCopied(false), 1500)
            })
          }}
        >
          {copied ? <Check size={15} color={colors.text2} /> : <Copy size={15} color={colors.text2} />}
        </Pressable>
      </View>
      <ScrollView horizontal showsHorizontalScrollIndicator={false}>
        <Text style={[styles.codeText, { color: colors.text }]} selectable>
          {code}
        </Text>
      </ScrollView>
    </View>
  )
}

function Table({ token, colors }: { token: Tokens.Table; colors: Colors }) {
  const cell = (c: Tokens.TableCell, head: boolean, key: number) => (
    <View key={key} style={[styles.cell, { borderColor: colors.line }]}>
      <Text style={[styles.cellText, { color: colors.text }, head && styles.strong]}>
        <Inline tokens={c.tokens} colors={colors} />
      </Text>
    </View>
  )
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.tableScroll}>
      <View style={[styles.table, { borderColor: colors.line }]}>
        <View style={[styles.row, { backgroundColor: colors.bubble }]}>{token.header.map((c, i) => cell(c, true, i))}</View>
        {token.rows.map((row, r) => (
          <View key={r} style={styles.row}>
            {row.map((c, i) => cell(c, false, i))}
          </View>
        ))}
      </View>
    </ScrollView>
  )
}

const styles = StyleSheet.create({
  root: { gap: 10 },
  heading: { fontWeight: '600', marginTop: 6 },
  paragraph: { fontSize: 16, lineHeight: 26 },
  strong: { fontWeight: '600' },
  em: { fontStyle: 'italic' },
  del: { textDecorationLine: 'line-through' },
  codespan: { fontFamily: MONO, fontSize: 14, borderRadius: 4 },
  link: { textDecorationLine: 'underline' },
  list: { gap: 6 },
  item: { flexDirection: 'row', gap: 8 },
  bullet: { fontSize: 16, lineHeight: 26, minWidth: 14 },
  itemBody: { flex: 1, gap: 6 },
  quote: { borderLeftWidth: 3, paddingLeft: 12, gap: 8 },
  hr: { height: StyleSheet.hairlineWidth, marginVertical: 6 },
  code: { borderRadius: 12, borderWidth: StyleSheet.hairlineWidth, overflow: 'hidden' },
  codeHead: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', paddingHorizontal: 12, paddingTop: 8 },
  codeLang: { fontSize: 12 },
  codeText: { fontFamily: MONO, fontSize: 13, lineHeight: 20, padding: 12 },
  tableScroll: { flexGrow: 0 },
  table: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 8, overflow: 'hidden' },
  row: { flexDirection: 'row' },
  // One width for every cell, so the columns line up row to row.
  cell: { width: 150, padding: 8, borderWidth: StyleSheet.hairlineWidth },
  cellText: { fontSize: 14, lineHeight: 20 },
})
