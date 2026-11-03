import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Pressable, StyleSheet, Text, View } from 'react-native'
import Svg, { Circle } from 'react-native-svg'

import { contextGauge } from '@openchat/chat-core/context-gauge'
import { queryKeys } from '@openchat/chat-core/query-keys'

import { useCompaction } from '@/hooks/compaction'
import { api, unwrap } from '@/lib/api'
import { useColors } from '@/theme'

const SIZE = 20
const STROKE = 2.5
const RADIUS = (SIZE - STROKE) / 2
const CIRCUMFERENCE = 2 * Math.PI * RADIUS
/** How long the figures stay up after a tap, unless tapped away sooner. */
const TIP_MS = 4000

/**
 * How full the model's context window is (the web's ContextMeter, figures from
 * chat-core's `contextGauge`): a ring beside the effort button, amber past 80%
 * and red past 95%. A tap shows the figures and notes above it, in the dark
 * chip the web's tooltip is, for a few seconds or until tapped again; nothing
 * there to press - compressing is only ever a run's doing. Nothing shows until
 * something has been answered, or for a model whose window is unknown.
 */
export function ContextMeter({ conversationId, limit }: { conversationId: string; limit?: number | null }) {
  const { t } = useTranslation()
  const colors = useColors()
  const usage = useQuery({
    queryKey: queryKeys.conversations.context(conversationId),
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/conversations/{conversationId}/context', { params: { path: { conversationId } }, signal })),
  })
  const summary = useCompaction(conversationId)
  const [open, setOpen] = useState(false)
  useEffect(() => {
    if (!open) return
    const timer = setTimeout(() => setOpen(false), TIP_MS)
    return () => clearTimeout(timer)
  }, [open])

  const gauge = contextGauge(usage.data?.usedTokens, limit)
  if (!gauge) return null
  const { percent } = gauge
  const tint = gauge.level === 'full' ? colors.errInk : gauge.level === 'high' ? colors.warning : colors.text2

  return (
    <View>
      {open ? (
        <View pointerEvents="none" style={[styles.tip, { backgroundColor: colors.text }]}>
          <Text style={[styles.tipLabel, { color: colors.bg }]}>{t('chat.context.label')}</Text>
          <Text style={[styles.tipHint, { color: colors.bg }]}>
            {t('chat.context.hint', { percent, left: 100 - percent, used: gauge.used, limit: gauge.limit })}
          </Text>
          {usage.data?.estimated ? <Text style={[styles.tipHint, styles.note, { color: colors.bg }]}>{t('chat.context.estimated')}</Text> : null}
          {summary ? <Text style={[styles.tipHint, styles.note, { color: colors.bg }]}>{t('chat.context.compacted')}</Text> : null}
        </View>
      ) : null}
      <Pressable
        onPress={() => setOpen(!open)}
        accessibilityRole="button"
        accessibilityLabel={t('chat.context.aria', { percent })}
        style={[styles.round, { backgroundColor: colors.bubble }]}
      >
        {/* Turned a quarter back, so it fills from the top. */}
        <Svg width={SIZE} height={SIZE} style={styles.ring}>
          <Circle cx={SIZE / 2} cy={SIZE / 2} r={RADIUS} fill="none" stroke={colors.text3} strokeOpacity={0.3} strokeWidth={STROKE} />
          <Circle
            cx={SIZE / 2}
            cy={SIZE / 2}
            r={RADIUS}
            fill="none"
            stroke={tint}
            strokeWidth={STROKE}
            strokeLinecap="round"
            strokeDasharray={CIRCUMFERENCE}
            strokeDashoffset={CIRCUMFERENCE * (1 - gauge.share)}
          />
        </Svg>
      </Pressable>
    </View>
  )
}

const styles = StyleSheet.create({
  round: { width: 44, height: 44, borderRadius: 22, alignItems: 'center', justifyContent: 'center' },
  ring: { transform: [{ rotate: '-90deg' }] },
  // Above the ring, reaching left from its right edge: the composer's right side holds the buttons.
  tip: { position: 'absolute', bottom: 52, right: 0, width: 220, borderRadius: 10, paddingHorizontal: 12, paddingVertical: 9, gap: 2 },
  tipLabel: { fontSize: 13, lineHeight: 18, fontWeight: '600' },
  tipHint: { fontSize: 12, lineHeight: 17, opacity: 0.8 },
  note: { marginTop: 4 },
})
