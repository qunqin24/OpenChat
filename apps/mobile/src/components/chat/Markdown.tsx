import * as Clipboard from 'expo-clipboard'
import * as Haptics from 'expo-haptics'
import { Check, Copy, Table2 as TableIcon } from 'lucide-react-native'
import { marked, type Token, type Tokens } from 'marked'
import { createContext, memo, useContext, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Linking, Pressable, ScrollView, StyleSheet, Text, View, type StyleProp, type TextStyle } from 'react-native'
import Animated, { FadeIn } from 'react-native-reanimated'
import Svg, { Defs, LinearGradient, Rect, Stop } from 'react-native-svg'

import { ARRIVAL_FADE_MS } from '@openchat/chat-core/arrival'
import { promoteInlineMath } from '@openchat/chat-core/inline-math'
import { isHttpUrl } from '@openchat/chat-core/urls'

import { FadingText } from './ArrivalFade'
import { useHighlighted } from './CodeHighlight'
import { useKeepInPlace } from './keep-in-place'
import { MermaidPreview } from './MermaidPreview'
import { PagePreview } from './PagePreview'
import { useColors, type Colors } from '@/theme'

const MONO = 'monospace'

/**
 * Markdown as the chat draws it: an answer's text, the reasoning. Shown as it
 * arrives, each batch fading in together (ArrivalFade.tsx, as the web does),
 * parsed with marked's lexer into blocks. Each block is its own memoised
 * element keyed by its source, so while an answer streams only the last block
 * - the one still growing - is drawn again. A block that appears after the
 * first render is new text, and fades in whole; `fresh`: the part began while
 * the reader watched, so even what it holds at first fades in.
 */
export function Markdown({ text, fresh = false }: { text: string; streaming?: boolean; fresh?: boolean }) {
  const blocks = useMemo(() => marked.lexer(promoteInlineMath(text), { gfm: true }), [text])
  const [first] = useState(() => blocks.length)
  return (
    <View style={styles.root}>
      {blocks.map((token, index) => (
        <Block key={index} token={token} raw={token.raw} fresh={fresh || index >= first} />
      ))}
    </View>
  )
}

/** Plain text that fades in as it arrives, for what is not Markdown (notes in the activity line). */
export function PlainText({ text, style }: { text: string; style?: StyleProp<TextStyle> }) {
  return <FadingText style={style}>{text}</FadingText>
}

/** Whether the block being drawn arrived while the reader watched. */
const Fresh = createContext(false)

const Block = memo(
  function Block({ token, fresh }: { token: Token; raw: string; fresh: boolean }) {
    const colors = useColors()
    return (
      <Fresh.Provider value={fresh}>
        <BlockView token={token} colors={colors} />
      </Fresh.Provider>
    )
  },
  // A block with the same source draws the same; its token object is new each parse.
  (a, b) => a.raw === b.raw && a.fresh === b.fresh,
)

/** A block that is not text - code, a table, a rule - arriving whole: it fades in as one. */
function Arriving({ children }: { children: ReactNode }) {
  const fresh = useContext(Fresh)
  return fresh ? <Animated.View entering={FadeIn.duration(ARRIVAL_FADE_MS)}>{children}</Animated.View> : children
}

function BlockView({ token, colors }: { token: Token; colors: Colors }): ReactNode {
  const fresh = useContext(Fresh)
  const text = { color: colors.text }
  switch (token.type) {
    case 'space':
      return null
    case 'heading': {
      const t = token as Tokens.Heading
      return (
        <FadingText fresh={fresh} style={[styles.heading, headingSize[t.depth] ?? headingSize[6], text]} accessibilityRole="header">
          <Inline tokens={t.tokens} colors={colors} />
        </FadingText>
      )
    }
    case 'paragraph': {
      const t = token as Tokens.Paragraph
      return (
        <FadingText fresh={fresh} style={[styles.paragraph, text]} selectable>
          <Inline tokens={t.tokens} colors={colors} />
        </FadingText>
      )
    }
    case 'text': {
      const t = token as Tokens.Text
      return (
        <FadingText fresh={fresh} style={[styles.paragraph, text]} selectable>
          {t.tokens ? <Inline tokens={t.tokens} colors={colors} /> : t.text}
        </FadingText>
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
      return (
        <Arriving>
          <CodeBlock code={(token as Tokens.Code).text} lang={(token as Tokens.Code).lang} colors={colors} />
        </Arriving>
      )
    case 'table':
      return (
        <Arriving>
          <Table token={token as Tokens.Table} colors={colors} />
        </Arriving>
      )
    case 'hr':
      return (
        <Arriving>
          <View style={[styles.hr, { backgroundColor: colors.line }]} />
        </Arriving>
      )
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

/** A longer block shows this many lines until it is opened. */
const CODE_FOLD = 18

function CodeBlock({ code, lang, colors }: { code: string; lang?: string; colors: Colors }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const [previewing, setPreviewing] = useState(false)
  const [open, setOpen] = useState(false)
  const keep = useKeepInPlace()
  const lines = code.split('\n')
  const folded = !open && lines.length > CODE_FOLD + 4
  const shown = folded ? lines.slice(0, CODE_FOLD).join('\n') : code
  const highlighted = useHighlighted(shown, lang)
  // An HTML block can be seen as the page it describes, a mermaid block as its diagram.
  const page = lang === 'html' || lang === 'htm'
  const diagram = lang === 'mermaid'
  return (
    <View onLayout={keep.onLayout} style={[styles.code, { backgroundColor: colors.code, borderColor: colors.line }]}>
      <View style={styles.codeHead}>
        <Text style={[styles.codeLang, { color: colors.text3 }]}>{lang || 'text'}</Text>
        <View style={styles.spacer} />
        {page || diagram ? (
          <Pressable hitSlop={10} onPress={() => setPreviewing(true)} accessibilityRole="button">
            <Text style={[styles.codeAction, { color: colors.text }]}>{diagram ? t('chat.diagram.show') : t('chat.preview.show')}</Text>
          </Pressable>
        ) : null}
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
      {/* Long lines scroll sideways rather than wrap, as code reads. */}
      <ScrollView horizontal showsHorizontalScrollIndicator={false}>
        <Text style={[styles.codeText, { color: colors.text }]} selectable>
          {highlighted}
        </Text>
      </ScrollView>
      {lines.length > CODE_FOLD + 4 ? (
        <Pressable
          onPress={() => {
            keep.expect()
            setOpen(!open)
          }}
          accessibilityRole="button"
          style={[styles.codeMore, { borderTopColor: colors.line }]}
        >
          <Text style={[styles.codeAction, { color: colors.text2 }]}>
            {open ? t('mobile.code-less') : t('mobile.code-more', { count: lines.length - CODE_FOLD })}
          </Text>
        </Pressable>
      ) : null}
      {previewing && diagram ? <MermaidPreview code={code} onClose={() => setPreviewing(false)} /> : null}
      {previewing && page ? <PagePreview title={t('chat.preview.title')} html={code} onClose={() => setPreviewing(false)} /> : null}
    </View>
  )
}

/** How wide a column may grow or shrink, so a short one stays readable and a long one wraps. */
const COLUMN_MIN = 72
const COLUMN_MAX = 240
/** Of a table too wide for its frame, the most of the frame one column may take. */
const COLUMN_SHARE = 0.42
const CELL_PAD = 12

/**
 * About how wide a line of cell text draws at 14 px: a CJK character takes a
 * square, anything else a little over half of one. React Native has no table
 * layout, so the columns are sized from this ahead of drawing and every row
 * uses the same widths, which keeps them lined up.
 */
function textWidth(text: string): number {
  let width = 0
  for (const ch of text) width += /[\u2e80-\u9fff\uac00-\ud7af\uff00-\uffef]/.test(ch) ? 14 : 7.6
  return width
}

/**
 * A Markdown table in an answer, in a frame of its own like a code block's: a
 * head that names it and how big it is, with a labelled copy button that puts
 * the table's Markdown on the clipboard (a cell's text cannot be selected
 * across cells, so this is how the table is taken elsewhere), and the table
 * under it, scrolling sideways when it is wider than the screen - a fade at
 * the right edge says so while there is more to see.
 */
function Table({ token, colors }: { token: Tokens.Table; colors: Colors }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const [frame, setFrame] = useState(0)
  const [content, setContent] = useState(0)
  const [scrolled, setScrolled] = useState(0)
  const copy = () => {
    void Haptics.selectionAsync()
    void Clipboard.setStringAsync(token.raw.trim()).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1600)
    })
  }
  const widths = useMemo(() => {
    const natural = token.header.map((head, c) => {
      const longest = Math.max(textWidth(head.text) + 8, ...token.rows.map((row) => textWidth(row[c]?.text ?? '')))
      return Math.min(COLUMN_MAX, Math.max(COLUMN_MIN, longest + CELL_PAD * 2))
    })
    const total = natural.reduce((sum, w) => sum + w, 0)
    if (frame <= 0) return natural.map(Math.round)
    // Room to spare: the columns share it, so the table fills its frame.
    if (total <= frame) return natural.map((w) => Math.floor((w * frame) / total))
    // Too wide: no column takes more than a share of the frame (its text wraps
    // instead), so part of the next one always shows and says there is more.
    const cap = Math.max(COLUMN_MIN, frame * COLUMN_SHARE)
    return natural.map((w) => Math.round(Math.min(w, cap)))
  }, [token, frame])
  const more = content - frame - scrolled > 4
  const cell = (c: Tokens.TableCell, head: boolean, key: number, last: boolean) => (
    <View key={key} style={[styles.cell, { width: widths[key], borderColor: colors.line }, last && styles.lastCell]}>
      <Text style={[styles.cellText, { color: head ? colors.text2 : colors.text }, head && styles.strong]}>
        <Inline tokens={c.tokens} colors={colors} />
      </Text>
    </View>
  )
  return (
    <View style={[styles.tableFrame, { borderColor: colors.line, backgroundColor: colors.bg }]}>
      <View style={[styles.tableHead, { borderBottomColor: colors.line }]}>
        <TableIcon size={14} color={colors.text3} />
        <Text style={[styles.codeLang, { color: colors.text3 }]}>
          {t('mobile.table')} · {t('mobile.table-size', { columns: token.header.length, rows: token.rows.length })}
        </Text>
        <View style={styles.spacer} />
        <Pressable onPress={copy} hitSlop={10} accessibilityRole="button" accessibilityLabel={t('chat.copy-table')} style={styles.tableCopy}>
          {copied ? <Check size={14} color={colors.text2} /> : <Copy size={14} color={colors.text2} />}
          <Text style={[styles.codeAction, { color: colors.text2 }]}>{copied ? t('mobile.copied') : t('chat.copy')}</Text>
        </Pressable>
      </View>
      <View onLayout={(event) => setFrame(event.nativeEvent.layout.width)}>
        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          style={styles.tableScroll}
          onContentSizeChange={(width) => setContent(width)}
          onScroll={(event) => setScrolled(event.nativeEvent.contentOffset.x)}
          scrollEventThrottle={32}
        >
          <View>
            <View style={[styles.row, { backgroundColor: colors.bubble }]}>{token.header.map((c, i) => cell(c, true, i, i === token.header.length - 1))}</View>
            {token.rows.map((row, r) => (
              <View key={r} style={[styles.row, { borderTopColor: colors.line }, styles.rowRule]}>
                {row.map((c, i) => cell(c, false, i, i === row.length - 1))}
              </View>
            ))}
          </View>
        </ScrollView>
        {more ? (
          <Svg pointerEvents="none" style={styles.tableFade} width={48} height="100%">
            <Defs>
              <LinearGradient id="fade" x1="0" y1="0" x2="1" y2="0">
                <Stop offset="0" stopColor={colors.bg} stopOpacity={0} />
                <Stop offset="1" stopColor={colors.bg} stopOpacity={0.95} />
              </LinearGradient>
            </Defs>
            <Rect x="0" y="0" width={48} height="100%" fill="url(#fade)" />
          </Svg>
        ) : null}
      </View>
    </View>
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
  code: { borderRadius: 16, borderWidth: StyleSheet.hairlineWidth, overflow: 'hidden' },
  codeHead: { flexDirection: 'row', alignItems: 'center', gap: 16, paddingHorizontal: 14, paddingTop: 10 },
  codeMore: { alignItems: 'center', paddingVertical: 10, borderTopWidth: StyleSheet.hairlineWidth },
  codeAction: { fontSize: 13, fontWeight: '500' },
  spacer: { flex: 1 },
  codeLang: { fontSize: 12 },
  codeText: { fontFamily: MONO, fontSize: 13, lineHeight: 20, paddingHorizontal: 14, paddingTop: 8, paddingBottom: 14 },
  tableFrame: { borderRadius: 16, borderWidth: StyleSheet.hairlineWidth, overflow: 'hidden' },
  tableHead: { flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 14, paddingVertical: 9, borderBottomWidth: StyleSheet.hairlineWidth },
  tableCopy: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  tableFade: { position: 'absolute', top: 0, right: 0, bottom: 0 },
  tableScroll: { flexGrow: 0 },
  row: { flexDirection: 'row' },
  rowRule: { borderTopWidth: StyleSheet.hairlineWidth },
  // Rules between columns only; the table's own border is the frame.
  cell: { paddingHorizontal: CELL_PAD, paddingVertical: 10, borderRightWidth: StyleSheet.hairlineWidth },
  lastCell: { borderRightWidth: 0 },
  cellText: { fontSize: 14, lineHeight: 21 },
})
