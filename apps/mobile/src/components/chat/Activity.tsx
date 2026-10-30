import { ChevronDown, ChevronRight, TriangleAlert } from 'lucide-react-native'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Linking, Pressable, StyleSheet, Text, View } from 'react-native'

import {
  activityHead,
  isFailure,
  searchHits,
  stepLabel,
  type ActivityItem,
  type ToolActivityStep,
} from '@openchat/chat-core/tool-activity'
import { isHttpUrl, siteName } from '@openchat/chat-core/urls'

import { useColors } from '@/theme'
import { PlainReveal } from './Markdown'
import { Pulse } from './Pulse'

/** Sources named in the collapsed line, before the rest are counted. */
const HEAD_SOURCES = 3

/**
 * How a reply was reached, folded into one line (the web's ToolActivity): what
 * the model is doing while it works, and what it did once it is done, with the
 * sites it found. Opening it lists every step in order, and what the model said
 * or thought between them.
 */
export function ActivityLine({ items, thinking }: { items: ActivityItem[]; thinking: 'reasoning' | 'waiting' | 'retrying' | false }) {
  const { t } = useTranslation()
  const colors = useColors()
  const [open, setOpen] = useState(false)
  const { title, active, failed, sources } = activityHead(t, items, thinking)
  const working = Boolean(active || thinking)
  const Chevron = open ? ChevronDown : ChevronRight
  const named = sources.slice(0, HEAD_SOURCES).map((hit) => siteName(hit.url))

  return (
    <View>
      <Pressable style={styles.head} onPress={() => setOpen(!open)} accessibilityRole="button" accessibilityState={{ expanded: open }}>
        {working ? <Pulse /> : null}
        <Text style={[styles.title, { color: colors.text2 }]} numberOfLines={1}>
          {title}
          {!working && failed > 0 ? <Text style={{ color: colors.errInk }}>{`  ${t('chat.tool.failures', { count: failed })}`}</Text> : null}
        </Text>
        <Chevron size={15} color={colors.text3} />
      </Pressable>
      {!open && named.length > 0 ? (
        <Text style={[styles.sources, { color: colors.text3 }]} numberOfLines={1}>
          {named.join(' · ')}
          {sources.length > HEAD_SOURCES ? ` +${sources.length - HEAD_SOURCES}` : ''}
        </Text>
      ) : null}
      {open ? (
        <View style={[styles.steps, { borderLeftColor: colors.line }]}>
          {items.map((item) =>
            item.kind === 'tool' ? (
              <Step key={item.key} step={item} />
            ) : (
              <PlainReveal
                key={item.key}
                text={item.text}
                streaming={item.streaming ?? false}
                style={[styles.note, { color: colors.text3 }, item.kind === 'reasoning' && styles.reasoning]}
              />
            ),
          )}
        </View>
      ) : null}
    </View>
  )
}

function Step({ step }: { step: ToolActivityStep }) {
  const { t } = useTranslation()
  const colors = useColors()
  const hits = searchHits(step.result)
  const failure = isFailure(step.status)
  const fetched =
    step.call.name === 'web_fetch' && step.result?.status === 'succeeded'
      ? (step.result.details as { url?: string; title?: string } | undefined)
      : undefined
  return (
    <View style={styles.step}>
      <View style={styles.stepHead}>
        {failure ? <TriangleAlert size={13} color={colors.errInk} /> : null}
        <Text style={[styles.stepLabel, { color: failure ? colors.errInk : colors.text2 }]}>{stepLabel(t, step, false)}</Text>
      </View>
      {hits.slice(0, 6).map((hit) => (
        <Text key={hit.url} style={[styles.hit, { color: colors.text3 }]} numberOfLines={1} onPress={() => void Linking.openURL(hit.url)}>
          {siteName(hit.url)} · {hit.title}
        </Text>
      ))}
      {fetched?.url ? (
        <Text
          style={[styles.hit, { color: colors.text3 }]}
          numberOfLines={1}
          onPress={isHttpUrl(fetched.url) ? () => void Linking.openURL(fetched.url!) : undefined}
        >
          {fetched.title || fetched.url}
        </Text>
      ) : null}
      {step.result && failure ? (
        <Text style={[styles.hit, { color: colors.text3 }]} numberOfLines={4}>
          {step.result.content}
        </Text>
      ) : null}
    </View>
  )
}

const styles = StyleSheet.create({
  head: { flexDirection: 'row', alignItems: 'center', gap: 8, minHeight: 28 },
  title: { flexShrink: 1, fontSize: 14, lineHeight: 20 },
  sources: { fontSize: 12, lineHeight: 17, marginTop: 2 },
  steps: { marginTop: 6, marginLeft: 4, paddingLeft: 12, borderLeftWidth: 1, gap: 10 },
  step: { gap: 3 },
  stepHead: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  stepLabel: { flexShrink: 1, fontSize: 13, lineHeight: 19 },
  hit: { fontSize: 12, lineHeight: 17 },
  note: { fontSize: 13, lineHeight: 20 },
  reasoning: { fontStyle: 'italic' },
})
