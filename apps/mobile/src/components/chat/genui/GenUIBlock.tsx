import {
  AlarmClock, ArrowDownRight, ArrowRight, ArrowUpRight, BookOpen, Brain, Briefcase, Building2, Calendar, Camera, Car,
  ChartColumn, Check, ChevronDown, ChevronRight, CircleAlert, CircleCheck, Cloud, Code, Coffee, Cpu, Database, Dumbbell,
  FileText, Film, Flag, Gift, Globe, GraduationCap, Heart, House, ImageIcon, Info, Leaf, Lightbulb, Link, Lock, Mail,
  MapPin, MessageCircle, Monitor, Music, Newspaper, Package, Palette, Phone, Plane, Rocket, Scale, Search, Server,
  Settings, ShieldCheck, ShoppingCart, Smartphone, Sparkles, Star, Stethoscope, Sun, Target, TrendingUp, TriangleAlert,
  Trophy, Users, Utensils, Wallet, Wrench, Zap, type LucideIcon,
} from 'lucide-react-native'
import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Linking, Pressable, ScrollView, StyleSheet, Text, View, type ViewStyle } from 'react-native'

import { choicesState } from '@openchat/chat-core/genui/choices'
import { parseBlock } from '@openchat/chat-core/genui/parse'
import { resolveBlock, type Cell, type UINode } from '@openchat/chat-core/genui/resolve'
import { isHttpUrl, siteName } from '@openchat/chat-core/urls'

import { useColors, type Colors } from '@/theme'
import { Markdown } from '../Markdown'
import { Pulse } from '../Pulse'
import { Chart, type ChartSeries } from './Chart'
import { Picture, usePicture, useViewer, type ShownPicture } from './pictures'

/**
 * What a Choices block in a reply can do, from the chat view around it:
 * `pick` sends a choice's message as the reader's next message (only on the
 * last answer, while nothing else is going on); `next` is what the reader sent
 * after this reply, to mark which choice it was.
 */
export interface ChoicesActions {
  pick?: (message: string) => void
  next?: string
}
export const ChoicesContext = createContext<ChoicesActions>({})

/** Whether the block is finished, for the components that act on it (Choices). */
const BlockComplete = createContext(true)
/** The page's colours and the viewer, handed down rather than asked for in every renderer. */
const Env = createContext<{ colors: Colors; zoom: (picture: ShownPicture, title: string, link?: string) => void } | null>(null)
const useEnv = () => useContext(Env)!

/**
 * One `openchat-ui` block, drawn natively (the web's genui/GenUIBlock.tsx).
 * Parsed and resolved again on every change; keys come from declaration
 * paths, so what is already drawn keeps its elements as the block grows.
 */
export function GenUIBlock({ source, complete }: { source: string; complete: boolean }) {
  const { t } = useTranslation()
  const colors = useColors()
  const { viewer, show } = useViewer()
  const [raw, setRaw] = useState(false)
  const resolved = useMemo(() => resolveBlock(parseBlock(source, complete), complete), [source, complete])
  const env = useMemo(() => ({ colors, zoom: show }), [colors, show])

  if (!resolved.root) {
    if (!complete) {
      return (
        <View style={[styles.pending, { backgroundColor: colors.bubble }]} accessibilityLabel={t('chat.genui.loading')}>
          <Pulse />
          <Text style={[styles.small, { color: colors.text3 }]}>{t('chat.genui.loading')}</Text>
        </View>
      )
    }
    // Nothing drawable: what the model wrote, folded away, rather than lost.
    return (
      <View style={[styles.fallback, { backgroundColor: colors.bubble }]}>
        <Pressable onPress={() => setRaw(!raw)} accessibilityRole="button">
          <Text style={[styles.small, { color: colors.text2 }]}>{t('chat.genui.failed')}</Text>
        </Pressable>
        {raw ? <Text style={[styles.mono, { color: colors.text2 }]}>{source}</Text> : null}
      </View>
    )
  }
  return (
    <Env.Provider value={env}>
      <BlockComplete.Provider value={complete}>
        <View style={styles.block}>{render(resolved.root)}</View>
        {viewer}
      </BlockComplete.Provider>
    </Env.Provider>
  )
}

type Props = Record<string, unknown>
const str = (value: unknown) => (typeof value === 'string' ? value : '')
const nodes = (value: unknown) => (Array.isArray(value) ? (value as UINode[]) : [])

function render(node: UINode): ReactNode {
  const Component = renderers[node.component]
  return Component ? <Component key={node.key} {...node.props} /> : null
}
const children = (value: unknown) => nodes(value).map(render)

const GAPS: Record<string, number> = { sm: 6, md: 12, lg: 20 }

const CARD_ICONS: Record<string, LucideIcon> = {
  brain: Brain, code: Code, shield: ShieldCheck, palette: Palette, rocket: Rocket, lightbulb: Lightbulb, book: BookOpen,
  'graduation-cap': GraduationCap, briefcase: Briefcase, chart: ChartColumn, 'trending-up': TrendingUp, wallet: Wallet,
  globe: Globe, 'map-pin': MapPin, calendar: Calendar, clock: AlarmClock, users: Users, heart: Heart, star: Star, zap: Zap,
  cpu: Cpu, database: Database, server: Server, cloud: Cloud, lock: Lock, mail: Mail, message: MessageCircle, phone: Phone,
  camera: Camera, music: Music, film: Film, plane: Plane, car: Car, home: House, 'shopping-cart': ShoppingCart, gift: Gift,
  coffee: Coffee, utensils: Utensils, leaf: Leaf, sun: Sun, wrench: Wrench, settings: Settings, search: Search,
  'file-text': FileText, flag: Flag, target: Target, trophy: Trophy, sparkles: Sparkles, package: Package,
  smartphone: Smartphone, monitor: Monitor, building: Building2, scale: Scale, stethoscope: Stethoscope,
  dumbbell: Dumbbell, newspaper: Newspaper, link: Link, check: CircleCheck, alert: TriangleAlert,
}

/** A tone's ink: info, success, warning, danger, or the quiet default. */
function toneColor(colors: Colors, tone: string) {
  if (tone === 'info') return colors.info
  if (tone === 'success') return colors.success
  if (tone === 'warning') return colors.warning
  if (tone === 'danger') return colors.errInk
  return colors.text2
}

const ASPECTS: Record<string, number> = { landscape: 4 / 3, square: 1, portrait: 3 / 4 }

const renderers: Record<string, (props: Props) => ReactNode> = {
  Card: (p) => <CardBlock {...p} />,
  Stack: (p) => <View style={{ gap: GAPS[str(p.gap)] ?? 12 }}>{children(p.children)}</View>,
  Row: (p) => (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: GAPS[str(p.gap)] ?? 12 }}>
      {children(p.children)}
    </ScrollView>
  ),
  Grid: (p) => <GridBlock columns={Math.min(Number(p.columns) || 2, 2)}>{children(p.children)}</GridBlock>,
  Heading: (p) => <HeadingBlock text={str(p.text)} level={Number(p.level) || 2} />,
  Text: (p) => <Markdown text={str(p.text)} />,
  Callout: (p) => <CalloutBlock tone={str(p.tone) || 'info'} title={str(p.title)} text={str(p.text)} />,
  Stat: (p) => <StatBlock {...p} />,
  Table: (p) => <TableBlock columns={(p.columns as string[] | undefined) ?? []} rows={(p.rows as Cell[][] | undefined) ?? []} caption={str(p.caption)} />,
  KeyValues: (p) => <KeyValuesBlock pairs={(p.pairs as [string, string][] | undefined) ?? []} />,
  Steps: (p) => <View style={styles.list}>{nodes(p.items).map((n, i) => <StepBlock key={n.key} {...n.props} index={i} />)}</View>,
  Step: (p) => <StepBlock {...p} index={0} />,
  Timeline: (p) => <View style={styles.list}>{children(p.items)}</View>,
  Event: (p) => <EventBlock time={str(p.time)} title={str(p.title)} detail={str(p.detail)} />,
  Tags: (p) => <TagsBlock items={(p.items as string[] | undefined) ?? []} />,
  Chart: (p) => (
    <Chart
      kind={(str(p.kind) || 'line') as 'line'}
      labels={(p.labels as string[] | undefined) ?? []}
      series={nodes(p.series).map((node): ChartSeries => ({ name: str(node.props.name), values: (node.props.values as number[] | undefined) ?? [] }))}
      title={str(p.title) || undefined}
      caption={str(p.caption) || undefined}
      unit={str(p.unit) || undefined}
    />
  ),
  Series: () => null,
  Tabs: (p) => <TabsBlock items={nodes(p.items)} />,
  Tab: (p) => <View style={styles.stack}>{children(p.children)}</View>,
  Details: (p) => <DetailsBlock title={str(p.title)}>{children(p.children)}</DetailsBlock>,
  Badge: (p) => <BadgeBlock text={str(p.text)} tone={str(p.tone)} />,
  Checklist: (p) => <ChecklistBlock title={str(p.title)} items={nodes(p.items)} />,
  Check: (p) => <ChecklistBlock title="" items={[{ key: 'check', component: 'Check', props: p }]} />,
  Image: (p) => <ImageBlock image={str(p.image) || str(p.src)} alt={str(p.alt)} caption={str(p.caption)} aspect={str(p.aspect) || 'landscape'} link={str(p.link)} />,
  Media: (p) => <MediaBlock image={str(p.image)} title={str(p.title)} detail={str(p.detail)} link={str(p.link)} />,
  Gallery: (p) => <GridBlock columns={Math.min(Number(p.columns) || 2, 3)}>{children(p.items)}</GridBlock>,
  Carousel: (p) => (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.carousel}>
      {nodes(p.items).map((n) => (
        <View key={n.key} style={styles.carouselItem}>
          {render(n)}
        </View>
      ))}
    </ScrollView>
  ),
  Article: (p) => <ArticleBlock title={str(p.title)} summary={str(p.summary)} link={str(p.link)} image={str(p.image)} category={str(p.category)} date={str(p.date)} />,
  Divider: () => <DividerBlock />,
  Choices: (p) => <ChoicesBlock question={str(p.question)} items={nodes(p.items)} action={str(p.action)} />,
  Choice: () => null,
}

function open(link: string) {
  if (isHttpUrl(link)) void Linking.openURL(link)
}

function CardBlock(p: Props) {
  const { colors } = useEnv()
  const Icon = CARD_ICONS[str(p.icon)]
  const body = children(p.children)
  const badge = p.badge as UINode | undefined
  const panel = body.length > 0
  const tone = str(p.tone)
  return (
    <View
      style={[
        styles.card,
        tone === 'muted'
          ? { backgroundColor: colors.bubble, borderColor: 'transparent' }
          : { backgroundColor: colors.card, borderColor: tone === 'accent' ? colors.text : colors.line },
      ]}
    >
      {str(p.image) ? <Cover image={str(p.image)} alt={str(p.title)} /> : null}
      {Icon && !panel && !str(p.image) ? <Icon size={22} color={colors.text2} /> : null}
      {str(p.title) || str(p.subtitle) || badge ? (
        <View style={styles.cardHead}>
          {Icon && panel ? <Icon size={18} color={colors.text2} /> : null}
          <View style={styles.flex}>
            {str(p.title) ? <Text style={[styles.cardTitle, { color: colors.text }]}>{str(p.title)}</Text> : null}
            {str(p.subtitle) ? <Text style={[styles.small, { color: colors.text3 }]}>{str(p.subtitle)}</Text> : null}
          </View>
          {badge ? render(badge) : null}
        </View>
      ) : null}
      {panel ? <View style={styles.stack}>{body}</View> : null}
      {str(p.footer) ? <Text style={[styles.small, { color: colors.text3 }]}>{str(p.footer)}</Text> : null}
    </View>
  )
}

/** Side by side, wrapped to `columns` per row - at most two or three on a phone. */
function GridBlock({ columns, children: items }: { columns: number; children: ReactNode[] }) {
  const width = `${100 / columns}%` as ViewStyle['width']
  return (
    <View style={styles.grid}>
      {items.map((item, i) => (
        <View key={i} style={[styles.gridCell, { width }]}>
          {item}
        </View>
      ))}
    </View>
  )
}

function HeadingBlock({ text, level }: { text: string; level: number }) {
  const { colors } = useEnv()
  const size = level === 1 ? 19 : level === 2 ? 17 : 15
  return (
    <Text style={{ fontSize: size, lineHeight: size + 7, fontWeight: '600', color: colors.text }} accessibilityRole="header">
      {text}
    </Text>
  )
}

const CALLOUT_ICONS: Record<string, LucideIcon> = { info: Info, success: CircleCheck, warning: TriangleAlert, danger: CircleAlert }

function CalloutBlock({ tone, title, text }: { tone: string; title: string; text: string }) {
  const { colors } = useEnv()
  const Icon = CALLOUT_ICONS[tone] ?? Info
  const ink = toneColor(colors, tone)
  return (
    <View style={[styles.callout, { backgroundColor: colors.bubble, borderLeftColor: ink }]}>
      <Icon size={18} color={ink} />
      <View style={styles.flex}>
        {title ? <Text style={[styles.cardTitle, { color: colors.text }]}>{title}</Text> : null}
        <Markdown text={text} />
      </View>
    </View>
  )
}

const TREND_ICONS: Record<string, LucideIcon> = { up: ArrowUpRight, down: ArrowDownRight, flat: ArrowRight }

function StatBlock(p: Props) {
  const { colors } = useEnv()
  const Icon = TREND_ICONS[str(p.trend)]
  const ink = toneColor(colors, str(p.tone) === 'good' ? 'success' : str(p.tone) === 'bad' ? 'danger' : '')
  return (
    <View style={[styles.stat, { backgroundColor: colors.bubble }]}>
      <Text style={[styles.small, { color: colors.text3 }]}>{str(p.label)}</Text>
      <Text style={[styles.statValue, { color: colors.text }]}>{str(p.value)}</Text>
      {str(p.change) ? (
        <View style={styles.inline}>
          {Icon ? <Icon size={14} color={ink} /> : null}
          <Text style={[styles.small, { color: ink }]}>{str(p.change)}</Text>
        </View>
      ) : null}
      {str(p.hint) ? <Text style={[styles.small, { color: colors.text3 }]}>{str(p.hint)}</Text> : null}
    </View>
  )
}

/** A cell written as a percentage from 0 to 100, as a number; otherwise null. */
function percent(cell: Cell | undefined): number | null {
  if (typeof cell !== 'string') return null
  const match = /^\s*(\d{1,3}(?:\.\d+)?)\s*%\s*$/.exec(cell)
  if (!match) return null
  const value = Number(match[1])
  return value <= 100 ? value : null
}

function TableBlock({ columns, rows, caption }: { columns: string[]; rows: Cell[][]; caption: string }) {
  const { colors } = useEnv()
  const every = (c: number, test: (cell: Cell | undefined) => boolean) => rows.length > 0 && rows.every((row) => test(row[c]))
  const kinds = columns.map((_, c) =>
    every(c, (cell) => percent(cell) !== null) ? 'progress' : every(c, (cell) => typeof cell === 'number') ? 'number' : null,
  )
  const cellStyle = (c: number) => [styles.cell, { borderColor: colors.line }, kinds[c] === 'number' && styles.right]
  return (
    <View style={styles.stack}>
      {caption ? <Text style={[styles.small, { color: colors.text2 }]}>{caption}</Text> : null}
      <ScrollView horizontal showsHorizontalScrollIndicator={false}>
        <View style={[styles.table, { borderColor: colors.line }]}>
          <View style={[styles.row, { backgroundColor: colors.bubble }]}>
            {columns.map((column, c) => (
              <Text key={c} style={[cellStyle(c), styles.strong, { color: colors.text }]}>
                {column}
              </Text>
            ))}
          </View>
          {rows.map((row, r) => (
            <View key={r} style={styles.row}>
              {columns.map((_, c) =>
                kinds[c] === 'progress' ? (
                  <View key={c} style={[styles.cell, { borderColor: colors.line }]}>
                    <Progress value={percent(row[c]) ?? 0} label={String(row[c])} />
                  </View>
                ) : (
                  <Text key={c} style={[cellStyle(c), { color: colors.text }]}>
                    {row[c] ?? ''}
                  </Text>
                ),
              )}
            </View>
          ))}
        </View>
      </ScrollView>
    </View>
  )
}

function Progress({ value, label }: { value: number; label: string }) {
  const { colors } = useEnv()
  return (
    <View style={styles.progress}>
      <View style={[styles.track, { backgroundColor: colors.line }]}>
        <View style={[styles.fill, { width: `${value}%`, backgroundColor: value >= 100 ? colors.success : colors.text }]} />
      </View>
      <Text style={[styles.small, { color: colors.text2 }]}>{label.trim()}</Text>
    </View>
  )
}

function KeyValuesBlock({ pairs }: { pairs: [string, string][] }) {
  const { colors } = useEnv()
  return (
    <View style={[styles.kv, { borderColor: colors.line }]}>
      {pairs.map(([label, value], i) => (
        <View key={i} style={[styles.kvRow, i > 0 && { borderTopColor: colors.line, borderTopWidth: StyleSheet.hairlineWidth }]}>
          <Text style={[styles.kvLabel, { color: colors.text3 }]}>{label}</Text>
          <Text style={[styles.kvValue, { color: colors.text }]} selectable>
            {value}
          </Text>
        </View>
      ))}
    </View>
  )
}

/** One step: numbered, or marked done / current / to-do when a plan's progress is tracked. */
function StepBlock(p: Props & { index: number }) {
  const { colors } = useEnv()
  const status = str(p.status)
  return (
    <View style={styles.step}>
      <View style={[styles.marker, { borderColor: status === 'current' ? colors.text : colors.line, backgroundColor: status === 'done' ? colors.text : 'transparent' }]}>
        {status === 'done' ? (
          <Check size={13} color={colors.bg} />
        ) : status ? null : (
          <Text style={[styles.markerText, { color: colors.text2 }]}>{p.index + 1}</Text>
        )}
      </View>
      {str(p.image) ? <Thumb image={str(p.image)} alt={str(p.title)} /> : null}
      <View style={styles.flex}>
        <Text style={[styles.cardTitle, { color: status === 'todo' ? colors.text3 : colors.text }]}>{str(p.title)}</Text>
        {str(p.detail) ? <Markdown text={str(p.detail)} /> : null}
      </View>
    </View>
  )
}

function EventBlock({ time, title, detail }: { time: string; title: string; detail: string }) {
  const { colors } = useEnv()
  return (
    <View style={[styles.event, { borderLeftColor: colors.line }]}>
      <Text style={[styles.small, { color: colors.text3 }]}>{time}</Text>
      <Text style={[styles.cardTitle, { color: colors.text }]}>{title}</Text>
      {detail ? <Markdown text={detail} /> : null}
    </View>
  )
}

function TagsBlock({ items }: { items: string[] }) {
  const { colors } = useEnv()
  return (
    <View style={styles.tags}>
      {items.map((tag, i) => (
        <Text key={i} style={[styles.tag, { backgroundColor: colors.bubble, color: colors.text2 }]}>
          {tag}
        </Text>
      ))}
    </View>
  )
}

function BadgeBlock({ text, tone }: { text: string; tone: string }) {
  const { colors } = useEnv()
  const ink = toneColor(colors, tone)
  return <Text style={[styles.badge, { color: ink, borderColor: ink }]}>{text}</Text>
}

function DividerBlock() {
  const { colors } = useEnv()
  return <View style={[styles.divider, { backgroundColor: colors.line }]} />
}

function TabsBlock({ items }: { items: UINode[] }) {
  const { colors } = useEnv()
  const tabs = items.filter((node) => node.component === 'Tab')
  const [active, setActive] = useState(0)
  const tab = tabs[Math.min(active, tabs.length - 1)]
  if (!tab) return null
  return (
    <View style={styles.stack}>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={[styles.tabs, { backgroundColor: colors.bubble }]}>
        {tabs.map((t, i) => (
          <Pressable
            key={t.key}
            onPress={() => setActive(i)}
            accessibilityRole="tab"
            accessibilityState={{ selected: i === active }}
            style={[styles.tab, i === active && { backgroundColor: colors.card }]}
          >
            <Text style={[styles.small, { color: i === active ? colors.text : colors.text2 }]}>{str(t.props.label)}</Text>
          </Pressable>
        ))}
      </ScrollView>
      <View style={styles.stack}>{children(tab.props.children)}</View>
    </View>
  )
}

function DetailsBlock({ title, children: content }: { title: string; children: ReactNode }) {
  const { colors } = useEnv()
  const [open, setOpen] = useState(false)
  const Chevron = open ? ChevronDown : ChevronRight
  return (
    <View style={[styles.details, { borderColor: colors.line }]}>
      <Pressable style={styles.detailsHead} onPress={() => setOpen(!open)} accessibilityRole="button" accessibilityState={{ expanded: open }}>
        <Text style={[styles.cardTitle, styles.flex, { color: colors.text }]}>{title}</Text>
        <Chevron size={16} color={colors.text3} />
      </Pressable>
      {open ? <View style={styles.stack}>{content}</View> : null}
    </View>
  )
}

/** A to-do list with its progress; ticking stays on this screen, the model's list is the answer. */
function ChecklistBlock({ title, items }: { title: string; items: UINode[] }) {
  const { t } = useTranslation()
  const { colors } = useEnv()
  const [ticked, setTicked] = useState<Record<string, boolean>>({})
  const checks = items.filter((node) => node.component === 'Check')
  const done = (node: UINode) => ticked[node.key] ?? node.props.done === true
  const count = checks.filter(done).length
  return (
    <View style={[styles.checklist, { backgroundColor: colors.bubble }]}>
      <View style={styles.cardHead}>
        {title ? <Text style={[styles.cardTitle, styles.flex, { color: colors.text }]}>{title}</Text> : <View style={styles.flex} />}
        <Text style={[styles.small, { color: colors.text3 }]}>{t('chat.genui.checklist-progress', { done: count, total: checks.length })}</Text>
      </View>
      {checks.map((node) => (
        <Pressable
          key={node.key}
          style={styles.check}
          accessibilityRole="checkbox"
          accessibilityState={{ checked: done(node) }}
          onPress={() => setTicked((state) => ({ ...state, [node.key]: !done(node) }))}
        >
          <View style={[styles.box, { borderColor: colors.text3, backgroundColor: done(node) ? colors.text : 'transparent' }]}>
            {done(node) ? <Check size={12} color={colors.bg} /> : null}
          </View>
          <Text style={[styles.checkText, { color: done(node) ? colors.text3 : colors.text }, done(node) && styles.struck]}>{str(node.props.text)}</Text>
        </Pressable>
      ))}
      <View style={[styles.track, { backgroundColor: colors.line }]}>
        <View style={[styles.fill, { width: `${checks.length ? (count / checks.length) * 100 : 0}%`, backgroundColor: colors.text }]} />
      </View>
    </View>
  )
}

/** A picture's frame while it is found, nothing if it never is; a tap shows it full screen. */
function ImageBlock({ image, alt, caption, aspect, link }: { image: string; alt: string; caption: string; aspect: string; link: string }) {
  const { t } = useTranslation()
  const { colors, zoom } = useEnv()
  const { picture, settled } = usePicture(image)
  const [missing, setMissing] = useState(false)
  if (missing || (settled && !picture)) return null
  const title = caption || alt || picture?.found?.title || (link ? siteName(link) : '') || t('viewer.image')
  return (
    <View style={styles.stack}>
      <Pressable
        disabled={!picture}
        onPress={() => picture && zoom(picture, title, link)}
        accessibilityLabel={t('viewer.open', { name: title })}
        style={[styles.frame, { aspectRatio: ASPECTS[aspect] ?? ASPECTS.landscape, backgroundColor: colors.bubble }]}
      >
        {picture ? <Picture picture={picture} style={StyleSheet.absoluteFill} onMissing={() => setMissing(true)} /> : null}
      </Pressable>
      {caption ? <Text style={[styles.small, { color: colors.text3 }]}>{caption}</Text> : null}
    </View>
  )
}

/** A small square picture beside a step or a Media row; a pale tile when there is none. */
function Thumb({ image, alt }: { image: string; alt: string }) {
  const { colors, zoom } = useEnv()
  const { picture, settled } = usePicture(image)
  const [missing, setMissing] = useState(false)
  const empty = missing || (settled && !picture)
  return (
    <Pressable disabled={!picture || empty} onPress={() => picture && zoom(picture, alt)} style={[styles.thumb, { backgroundColor: colors.bubble }]}>
      {picture && !empty ? <Picture picture={picture} style={StyleSheet.absoluteFill} onMissing={() => setMissing(true)} /> : null}
      {empty ? <ImageIcon size={18} color={colors.text3} /> : null}
    </Pressable>
  )
}

function Cover({ image, alt }: { image: string; alt: string }) {
  const { colors, zoom } = useEnv()
  const { picture, settled } = usePicture(image)
  const [missing, setMissing] = useState(false)
  const empty = missing || (settled && !picture)
  return (
    <Pressable disabled={!picture || empty} onPress={() => picture && zoom(picture, alt)} style={[styles.cover, { backgroundColor: colors.bubble }]}>
      {picture && !empty ? <Picture picture={picture} style={StyleSheet.absoluteFill} onMissing={() => setMissing(true)} /> : null}
      {empty ? <ImageIcon size={22} color={colors.text3} /> : null}
    </Pressable>
  )
}

function MediaBlock({ image, title, detail, link }: { image: string; title: string; detail: string; link: string }) {
  const { colors } = useEnv()
  return (
    <View style={styles.media}>
      <Thumb image={image} alt={title} />
      <View style={styles.flex}>
        <Text style={[styles.cardTitle, { color: colors.text }, isHttpUrl(link) && styles.underline]} onPress={isHttpUrl(link) ? () => open(link) : undefined}>
          {title}
        </Text>
        {detail ? <Markdown text={detail} /> : null}
      </View>
    </View>
  )
}

function ArticleBlock({ title, summary, link, image, category, date }: { title: string; summary: string; link: string; image: string; category: string; date: string }) {
  const { colors } = useEnv()
  const { picture, settled } = usePicture(image)
  const [failed, setFailed] = useState(false)
  const missing = !image || failed || (settled && !picture)
  return (
    <View style={[styles.article, { borderColor: colors.line }]}>
      {!missing ? (
        <Pressable onPress={() => open(link)} style={[styles.frame, { aspectRatio: 16 / 9, backgroundColor: colors.bubble }]}>
          {picture ? <Picture picture={picture} style={StyleSheet.absoluteFill} onMissing={() => setFailed(true)} /> : null}
        </Pressable>
      ) : null}
      <View style={styles.articleBody}>
        {category || date || link ? (
          <Text style={[styles.small, { color: colors.text3 }]}>{[category, link ? siteName(link) : '', date].filter(Boolean).join(' · ')}</Text>
        ) : null}
        <Text style={[styles.cardTitle, { color: colors.text }]} onPress={isHttpUrl(link) ? () => open(link) : undefined}>
          {title}
        </Text>
        {summary ? <Markdown text={summary} /> : null}
      </View>
    </View>
  )
}

/** At most this many choices are drawn, as the catalogue tells the model. */
const MAX_CHOICES = 4

/**
 * A question with a few directions to take next, each sending a message
 * written for it. Only the last answer's choices send; once the conversation
 * has moved on, the card stays with the choice that was sent marked.
 */
function ChoicesBlock({ question, items, action }: { question: string; items: UINode[]; action: string }) {
  const { t } = useTranslation()
  const { colors } = useEnv()
  const { pick, next } = useContext(ChoicesContext)
  const complete = useContext(BlockComplete)
  const options = items
    .slice(0, MAX_CHOICES)
    .map((item) => ({ key: item.key, label: str(item.props.label), message: str(item.props.message) }))
    .filter((option) => option.label && option.message)
  const [selected, setSelected] = useState<number | null>(null)
  const { taken, open: offered, live } = choicesState(options.map((option) => option.message), { complete, next, canPick: Boolean(pick) })
  const marked = taken ?? selected
  return (
    <View style={[styles.choices, { borderColor: colors.line }, !live && styles.settled]}>
      <Text style={[styles.cardTitle, { color: colors.text }]}>{question}</Text>
      {options.map((option, index) => (
        <Pressable
          key={option.key}
          disabled={!live}
          onPress={() => setSelected(index)}
          accessibilityRole="radio"
          accessibilityState={{ checked: marked === index, disabled: !live }}
          style={[styles.choice, { borderColor: marked === index ? colors.text : colors.line, backgroundColor: marked === index ? colors.field : 'transparent' }]}
        >
          <View style={[styles.dot, { borderColor: marked === index ? colors.text : colors.text3 }]}>
            {marked === index ? <View style={[styles.dotFill, { backgroundColor: colors.text }]} /> : null}
          </View>
          <Text style={[styles.checkText, { color: colors.text }]}>{option.label}</Text>
        </Pressable>
      ))}
      {offered ? (
        <Pressable
          disabled={!live || selected === null}
          onPress={() => {
            const option = selected === null ? undefined : options[selected]
            if (option && pick) pick(option.message)
          }}
          style={[styles.send, { backgroundColor: colors.accent }, (!live || selected === null) && styles.settled]}
          accessibilityRole="button"
        >
          <Text style={[styles.sendText, { color: colors.accentFg }]}>{action || t('chat.genui.choices-send')}</Text>
          <ArrowRight size={16} color={colors.accentFg} />
        </Pressable>
      ) : null}
    </View>
  )
}

const styles = StyleSheet.create({
  block: { gap: 12 },
  pending: { flexDirection: 'row', alignItems: 'center', gap: 8, padding: 14, borderRadius: 16 },
  fallback: { padding: 14, borderRadius: 16, gap: 8 },
  mono: { fontFamily: 'monospace', fontSize: 12, lineHeight: 18 },
  small: { fontSize: 13, lineHeight: 18 },
  stack: { gap: 12 },
  flex: { flex: 1 },
  inline: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  card: { borderRadius: 18, borderWidth: StyleSheet.hairlineWidth, padding: 14, gap: 10, overflow: 'hidden' },
  cardHead: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  cardTitle: { fontSize: 15, lineHeight: 21, fontWeight: '600' },
  grid: { flexDirection: 'row', flexWrap: 'wrap', marginHorizontal: -5 },
  gridCell: { padding: 5 },
  callout: { flexDirection: 'row', gap: 10, padding: 12, borderRadius: 14, borderLeftWidth: 3 },
  stat: { padding: 14, borderRadius: 16, gap: 2, minWidth: 140 },
  statValue: { fontSize: 24, lineHeight: 30, fontWeight: '600' },
  table: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 10, overflow: 'hidden' },
  row: { flexDirection: 'row' },
  cell: { width: 130, padding: 8, borderWidth: StyleSheet.hairlineWidth, fontSize: 14, lineHeight: 20 },
  right: { textAlign: 'right' },
  strong: { fontWeight: '600' },
  progress: { gap: 4 },
  track: { height: 6, borderRadius: 3, overflow: 'hidden' },
  fill: { height: 6, borderRadius: 3 },
  kv: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 12 },
  kvRow: { flexDirection: 'row', gap: 12, paddingHorizontal: 12, paddingVertical: 10 },
  kvLabel: { width: 96, fontSize: 14, lineHeight: 20 },
  kvValue: { flex: 1, fontSize: 14, lineHeight: 20 },
  list: { gap: 14 },
  step: { flexDirection: 'row', gap: 12 },
  marker: { width: 24, height: 24, borderRadius: 12, borderWidth: 1.5, alignItems: 'center', justifyContent: 'center', marginTop: -1 },
  markerText: { fontSize: 12, fontWeight: '600' },
  event: { borderLeftWidth: 2, paddingLeft: 12, gap: 2 },
  tags: { flexDirection: 'row', flexWrap: 'wrap', gap: 6 },
  tag: { fontSize: 13, paddingHorizontal: 10, paddingVertical: 4, borderRadius: 12, overflow: 'hidden' },
  badge: { fontSize: 12, paddingHorizontal: 8, paddingVertical: 2, borderRadius: 10, borderWidth: 1, overflow: 'hidden' },
  divider: { height: StyleSheet.hairlineWidth },
  tabs: { borderRadius: 12, padding: 3, gap: 2 },
  tab: { paddingHorizontal: 12, paddingVertical: 6, borderRadius: 9 },
  details: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 14, padding: 12, gap: 10 },
  detailsHead: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  checklist: { borderRadius: 16, padding: 14, gap: 10 },
  check: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  box: { width: 18, height: 18, borderRadius: 5, borderWidth: 1.5, alignItems: 'center', justifyContent: 'center' },
  checkText: { flex: 1, fontSize: 15, lineHeight: 21 },
  struck: { textDecorationLine: 'line-through' },
  frame: { borderRadius: 14, overflow: 'hidden', width: '100%' },
  thumb: { width: 64, height: 64, borderRadius: 10, overflow: 'hidden', alignItems: 'center', justifyContent: 'center' },
  cover: { height: 120, marginHorizontal: -14, marginTop: -14, alignItems: 'center', justifyContent: 'center', overflow: 'hidden' },
  media: { flexDirection: 'row', gap: 12 },
  underline: { textDecorationLine: 'underline' },
  article: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 16, overflow: 'hidden' },
  articleBody: { padding: 12, gap: 6 },
  carousel: { gap: 12 },
  carouselItem: { width: 260 },
  choices: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 18, padding: 14, gap: 10 },
  settled: { opacity: 0.55 },
  choice: { flexDirection: 'row', alignItems: 'center', gap: 10, borderWidth: 1, borderRadius: 14, padding: 12 },
  dot: { width: 18, height: 18, borderRadius: 9, borderWidth: 1.5, alignItems: 'center', justifyContent: 'center' },
  dotFill: { width: 8, height: 8, borderRadius: 4 },
  send: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 6, height: 44, borderRadius: 22 },
  sendText: { fontSize: 15, fontWeight: '500' },
})
