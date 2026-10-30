import { AlarmClock, Brain, ChevronDown, ChevronRight, FileSearch, FileText, NotebookPen, Search, TriangleAlert, Workflow, Wrench } from 'lucide-react-native'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Image, Linking, Pressable, StyleSheet, Text, View } from 'react-native'
import Svg, { Defs, LinearGradient, Rect, Stop } from 'react-native-svg'

import {
  activityHead,
  activitySegments,
  isFailure,
  plainSnippet,
  searchHits,
  stepLabel,
  type ActivityItem,
  type SearchHit,
  type ToolActivityStep,
} from '@openchat/chat-core/tool-activity'
import { isHttpUrl, siteName } from '@openchat/chat-core/urls'

import { serverUrl } from '@/config'
import { currentToken } from '@/lib/token'
import { Sheet } from '@/components/Sheet'
import { useColors } from '@/theme'
import { useKeepInPlace } from './keep-in-place'
import { Markdown } from './Markdown'
import { Pulse } from './Pulse'

/** Source marks stacked in the collapsed line, before the rest are counted. */
const HEAD_SOURCES = 4

/**
 * How a reply was reached - its process (chat-core/reply-process.ts) - folded
 * into one line, as the web's ToolActivity: what the model is doing while it
 * works, and once it is done only how long it thought, as ChatGPT's "thought
 * for 20s" (a fold without thinking says what it did, with the sites it drew
 * on). Opened, everything lies flush with the answer, no rail down its side:
 * the words the model said on its way as it said them, and between those
 * words each run of thinking and steps folded into a line of its own
 * (`nested`); with nothing said on the way, the thinking and the steps
 * themselves. A search is the pages it found (a tap lists them), a page read
 * its site.
 */
export function ActivityLine({
  items,
  thinking,
  nested = false,
}: {
  items: ActivityItem[]
  thinking: 'reasoning' | 'waiting' | 'retrying' | false
  nested?: boolean
}) {
  const { t } = useTranslation()
  const colors = useColors()
  const { title, active, failed, sources, thoughtOnly } = activityHead(t, items, thinking, !nested)
  const segments = activitySegments(items)
  // Open while the model works, so the reader watches it think and search;
  // folded away the moment the answer starts, so the answer is what is left
  // (as ChatGPT has it). Either way the reader may open or close it.
  const working = Boolean(active || thinking)
  const [open, setOpen] = useState(working)
  const [wasWorking, setWasWorking] = useState(working)
  if (working !== wasWorking) {
    setWasWorking(working)
    setOpen(working)
  }
  const Chevron = open ? ChevronDown : ChevronRight
  const marks = thoughtOnly ? [] : sources.slice(0, HEAD_SOURCES)
  const keep = useKeepInPlace()
  if (items.length === 0 && !working) return null

  return (
    <View onLayout={keep.onLayout}>
      <Pressable
        style={styles.head}
        onPress={() => {
          keep.expect()
          setOpen(!open)
        }}
        accessibilityRole="button"
        accessibilityState={{ expanded: open }}
      >
        {working ? <Pulse /> : null}
        <Text style={[styles.title, nested && styles.nestedTitle, { color: colors.text2 }]} numberOfLines={1}>
          {title}
          {!working && !thoughtOnly && failed > 0 ? <Text style={{ color: colors.errInk }}>{`  ${t('chat.tool.failures', { count: failed })}`}</Text> : null}
        </Text>
        {!open && marks.length > 0 ? (
          <View style={styles.marks}>
            {marks.map((hit, i) => (
              <View key={hit.url} style={i > 0 && styles.overlap}>
                <SourceMark url={hit.url} size={18} ring />
              </View>
            ))}
            {sources.length > HEAD_SOURCES ? <Text style={[styles.more, { color: colors.text3 }]}>+{sources.length - HEAD_SOURCES}</Text> : null}
          </View>
        ) : null}
        <Chevron size={15} color={colors.text3} />
      </Pressable>
      {open ? (
        <View style={[styles.timeline, nested && styles.nestedTimeline]}>
          {segments
            ? segments.map((segment, i) =>
                segment.kind === 'note' ? (
                  <Moment key={segment.item.key} item={segment.item} working={working} />
                ) : (
                  <ActivityLine key={segment.key} items={segment.items} thinking={i === segments.length - 1 ? thinking : false} nested />
                ),
              )
            : items.map((item) => <Moment key={item.key} item={item} working={working} />)}
        </View>
      ) : null}
    </View>
  )
}

/**
 * One thing that happened: a step beside its tool's icon; a thought, a step
 * quieter than the answer; words the model said on its way, as the answer's.
 */
function Moment({ item, working }: { item: ActivityItem; working: boolean }) {
  if (item.kind === 'tool')
    return (
      <View style={styles.step}>
        <View style={styles.markBox}>
          <StepMark step={item} />
        </View>
        <View style={styles.stepBody}>
          <Step step={item} />
        </View>
      </View>
    )
  if (item.kind === 'reasoning') return <Thought text={item.text} streaming={item.streaming ?? false} fresh={item.fresh ?? false} peek={working} />
  // Fresh only while it streams: the timeline mounts again each time the
  // reader opens it, and finished words are not faded in again.
  return <Markdown text={item.text} streaming={item.streaming} fresh={(item.fresh ?? false) && (item.streaming ?? false)} />
}

/** How tall a thought's window is while the model works. */
const PEEK = 220

/**
 * A thought. While the model works only its last lines show, in a window kept
 * at the newest, faded at the top where it is cut (as the web's Thought), so a
 * long one does not push what follows a screen away; once it is done the fold
 * has closed, and opened again it shows the whole of it. Never a box that
 * scrolls on its own, which would take the swipe from the transcript.
 */
function Thought({ text, streaming, fresh, peek }: { text: string; streaming: boolean; fresh: boolean; peek: boolean }) {
  const colors = useColors()
  const [height, setHeight] = useState(0)
  const cut = peek && height > PEEK
  return (
    <View style={[styles.thought, peek && styles.peek]}>
      <View onLayout={(event) => setHeight(event.nativeEvent.layout.height)}>
        <Markdown text={text} streaming={streaming} fresh={fresh && streaming} />
      </View>
      {cut ? (
        <Svg style={styles.peekFade} width="100%" height={56} pointerEvents="none">
          <Defs>
            <LinearGradient id="peek" x1="0" y1="0" x2="0" y2="1">
              <Stop offset="0" stopColor={colors.bg} stopOpacity={1} />
              <Stop offset="1" stopColor={colors.bg} stopOpacity={0} />
            </LinearGradient>
          </Defs>
          <Rect width="100%" height="100%" fill="url(#peek)" />
        </Svg>
      ) : null}
    </View>
  )
}

function StepMark({ step }: { step: ToolActivityStep }) {
  const colors = useColors()
  if (isFailure(step.status)) return <TriangleAlert size={15} color={colors.errInk} />
  return stepIcon(step.call.name, colors.text3)
}

/**
 * A step: a search as how many pages it found, their icons stacked, and the
 * query under it - a tap opens the results; a page read as the site, its icon,
 * and a tap opens the page; anything else as its label.
 */
function Step({ step }: { step: ToolActivityStep }) {
  const { t } = useTranslation()
  const colors = useColors()
  const [listing, setListing] = useState(false)
  const hits = searchHits(step.result)
  const failure = isFailure(step.status)
  const fetched =
    step.call.name === 'web_fetch' && step.result?.status === 'succeeded'
      ? (step.result.details as { url?: string; title?: string } | undefined)
      : undefined
  const query = step.call.name === 'web_search' ? String((step.result?.details as { query?: unknown } | undefined)?.query ?? '') : ''

  if (hits.length > 0) {
    return (
      <>
        <Pressable onPress={() => setListing(true)} accessibilityRole="button" style={styles.found}>
          <View style={styles.foundHead}>
            <Text style={[styles.stepLabel, { color: colors.text2 }]}>{t('mobile.search-found', { count: hits.length })}</Text>
            <View style={styles.marks}>
              {onePerSite(hits).slice(0, HEAD_SOURCES).map((hit, i) => (
                <View key={hit.url} style={i > 0 && styles.overlap}>
                  <SourceMark url={hit.url} size={18} ring />
                </View>
              ))}
            </View>
            <ChevronRight size={14} color={colors.text3} />
          </View>
          {query ? (
            <Text style={[styles.query, { color: colors.text3 }]} numberOfLines={1}>
              {query}
            </Text>
          ) : null}
        </Pressable>
        <Sheet open={listing} onClose={() => setListing(false)} title={query || t('mobile.search-results')}>
          {hits.map((hit) => (
            <Result key={hit.url} hit={hit} />
          ))}
        </Sheet>
      </>
    )
  }

  if (fetched?.url) {
    const host = siteName(fetched.url)
    return (
      <Pressable onPress={isHttpUrl(fetched.url) ? () => void Linking.openURL(fetched.url!) : undefined} style={styles.foundHead}>
        <Text style={[styles.stepLabel, { color: colors.text2 }]} numberOfLines={1}>
          {t('mobile.browsed', { host })}
        </Text>
        <SourceMark url={fetched.url} size={18} />
      </Pressable>
    )
  }

  return (
    <View style={styles.found}>
      <Text style={[styles.stepLabel, { color: failure ? colors.errInk : colors.text2 }]}>{stepLabel(t, step, false)}</Text>
      {/* A failure's explanation is the text the model read: written for the
          model, in English, but the only account of what went wrong. */}
      {step.result && failure ? (
        <Text style={[styles.error, { color: colors.text3 }]} numberOfLines={4}>
          {step.result.content}
        </Text>
      ) : null}
    </View>
  )
}

/** One search result in the list: the site and when, the page's title, and a little of what it says. */
function Result({ hit }: { hit: SearchHit & { snippet?: string; published?: string } }) {
  const { i18n } = useTranslation()
  const colors = useColors()
  const day = hit.published ? formatDay(hit.published, i18n.language) : ''
  const snippet = hit.snippet ? plainSnippet(hit.snippet) : ''
  return (
    <Pressable
      onPress={isHttpUrl(hit.url) ? () => void Linking.openURL(hit.url) : undefined}
      accessibilityRole="link"
      style={({ pressed }) => [styles.result, pressed && { backgroundColor: colors.field }]}
    >
      <View style={styles.resultSite}>
        <SourceMark url={hit.url} size={20} />
        <Text style={[styles.resultHost, { color: colors.text }]} numberOfLines={1}>
          {siteName(hit.url)}
        </Text>
        {day ? <Text style={[styles.resultDay, { color: colors.text3 }]}>{day}</Text> : null}
      </View>
      <Text style={[styles.resultTitle, { color: colors.text }]} numberOfLines={2}>
        {hit.title || hit.url}
      </Text>
      {snippet ? (
        <Text style={[styles.resultSnippet, { color: colors.text3 }]} numberOfLines={2}>
          {snippet}
        </Text>
      ) : null}
    </Pressable>
  )
}

/** The hits with one per site, in order: four marks of the same site say less than four sites. */
function onePerSite(hits: SearchHit[]): SearchHit[] {
  const seen = new Set<string>()
  return hits.filter((hit) => {
    const site = siteName(hit.url)
    if (seen.has(site)) return false
    seen.add(site)
    return true
  })
}

/** A published day (YYYY-MM-DD) as the reader's language writes it. */
function formatDay(day: string, language: string): string {
  const date = new Date(`${day}T00:00:00`)
  if (Number.isNaN(date.getTime())) return day
  try {
    return new Intl.DateTimeFormat(language, { year: 'numeric', month: 'long', day: 'numeric' }).format(date)
  } catch {
    return day
  }
}

function stepIcon(name: string, color: string) {
  const props = { size: 15, color }
  if (name === 'web_search') return <Search {...props} />
  if (name === 'web_fetch') return <FileText {...props} />
  if (name === 'file_read') return <FileText {...props} />
  if (name === 'file_search') return <FileSearch {...props} />
  if (name === 'memory') return <Brain {...props} />
  if (name === 'scheduled_tasks') return <AlarmClock {...props} />
  if (name === 'document') return <NotebookPen {...props} />
  if (name.startsWith('subagent')) return <Workflow {...props} />
  return <Wrench {...props} />
}

/**
 * A source's mark (the web's SourceMark): the site's icon, through our own
 * server - which fetches and caches it, so the phone asks no third party about
 * what the answer drew on - over the site's first letter, which stays where
 * there is no icon (the server answers those with a 1×1 picture).
 */
export function SourceMark({ url, size, ring }: { url: string; size: number; ring?: boolean }) {
  const colors = useColors()
  const domain = siteName(url)
  const [failed, setFailed] = useState(false)
  const token = currentToken()
  return (
    <View
      style={[
        styles.mark,
        { width: size, height: size, borderRadius: size / 2, backgroundColor: colors.panel },
        ring && { borderWidth: 1.5, borderColor: colors.bg },
      ]}
    >
      <Text style={[styles.markLetter, { color: colors.text2, fontSize: size * 0.55 }]}>{domain.charAt(0).toUpperCase() || '·'}</Text>
      {!failed && domain ? (
        <Image
          source={{ uri: `${serverUrl}/api/v1/favicons/${encodeURIComponent(domain)}`, headers: token ? { Authorization: `Bearer ${token}` } : undefined }}
          style={[StyleSheet.absoluteFill, { backgroundColor: colors.bg }]}
          onError={() => setFailed(true)}
          onLoad={(event) => {
            if (event.nativeEvent.source.width <= 1) setFailed(true)
          }}
        />
      ) : null}
    </View>
  )
}

const styles = StyleSheet.create({
  head: { flexDirection: 'row', alignItems: 'center', gap: 8, minHeight: 30 },
  title: { flexShrink: 1, fontSize: 14, lineHeight: 20 },
  marks: { flexDirection: 'row', alignItems: 'center' },
  overlap: { marginLeft: -5 },
  more: { fontSize: 12, marginLeft: 4 },
  nestedTitle: { fontSize: 13 },
  // More room under the open process than between its lines: the answer starts there.
  timeline: { marginTop: 10, marginBottom: 6, gap: 12 },
  nestedTimeline: { marginBottom: 0 },
  step: { flexDirection: 'row', gap: 10 },
  markBox: { width: 18, height: 22, justifyContent: 'center', alignItems: 'center' },
  stepBody: { flex: 1, minWidth: 0 },
  // A step quieter than the answer.
  thought: { opacity: 0.6 },
  peek: { maxHeight: PEEK, overflow: 'hidden', justifyContent: 'flex-end' },
  peekFade: { position: 'absolute', top: 0, left: 0 },
  found: { gap: 3 },
  foundHead: { flexDirection: 'row', alignItems: 'center', gap: 8, minHeight: 22 },
  stepLabel: { flexShrink: 1, fontSize: 14, lineHeight: 22 },
  query: { fontSize: 12, lineHeight: 17 },
  error: { fontSize: 12, lineHeight: 17 },
  result: { gap: 6, paddingHorizontal: 12, paddingVertical: 14, borderRadius: 16 },
  resultSite: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  resultHost: { flexShrink: 1, fontSize: 13, fontWeight: '600' },
  resultDay: { fontSize: 12 },
  resultTitle: { fontSize: 16, lineHeight: 23, fontWeight: '600' },
  resultSnippet: { fontSize: 14, lineHeight: 21 },
  mark: { alignItems: 'center', justifyContent: 'center', overflow: 'hidden' },
  markLetter: { fontWeight: '600' },
})
