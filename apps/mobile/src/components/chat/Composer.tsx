import type { ChatModel, ReasoningEffort } from '@openchat/api-client'
import { ArrowUp, Brain, Check, ChevronDown, Globe, Square } from 'lucide-react-native'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native'

import { Sheet, SheetOption } from '@/components/Sheet'
import { useColors } from '@/theme'

interface Props {
  value: string
  onChange: (value: string) => void
  onSend: () => void
  /** A run is going: the button stops it. */
  onStop?: () => void
  busy: boolean
  /** The conversation ends on a refused message: this is its editor. */
  revising: boolean
  models: ChatModel[]
  modelId: string
  onPickModel: (id: string) => void
  thinkingLevels: ReasoningEffort[]
  effort: ReasoningEffort
  onPickEffort: (effort: ReasoningEffort) => void
  canSearch: boolean
  webSearch: boolean
  onToggleWebSearch: () => void
}

/**
 * The message box and what the next run is asked with: the model, how hard it
 * thinks, and whether it may go on the web (the web's Composer and its
 * pickers). The send button stops a run while one is going.
 */
export function Composer(props: Props) {
  const { t } = useTranslation()
  const colors = useColors()
  const [picking, setPicking] = useState<'model' | 'effort' | null>(null)
  const model = props.models.find((m) => m.id === props.modelId)
  const canSend = props.value.trim().length > 0 && !props.busy
  const stopping = Boolean(props.onStop)

  return (
    <View style={[styles.box, { backgroundColor: colors.bubble }]}>
      {props.revising ? <Text style={[styles.revise, { color: colors.errInk }]}>{t('chat.revise.label')}</Text> : null}
      <TextInput
        style={[styles.input, { color: colors.text }]}
        placeholder={t('chat.placeholder')}
        placeholderTextColor={colors.text3}
        value={props.value}
        onChangeText={props.onChange}
        multiline
      />
      <View style={styles.row}>
        <Chip onPress={() => setPicking('model')} label={model?.name ?? (props.modelId || t('chat.model.pick'))} chevron />
        {props.thinkingLevels.length > 0 ? (
          <Chip
            onPress={() => setPicking('effort')}
            icon={<Brain size={15} color={colors.text2} />}
            label={t(`chat.thinking.${props.effort}`)}
          />
        ) : null}
        {props.canSearch ? (
          <Chip
            onPress={props.onToggleWebSearch}
            icon={<Globe size={15} color={props.webSearch ? colors.accentFg : colors.text2} />}
            label={t('chat.web-search.label')}
            active={props.webSearch}
          />
        ) : null}
        <View style={styles.spacer} />
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={stopping ? t('chat.stop') : t('chat.send')}
          disabled={!stopping && !canSend}
          onPress={stopping ? props.onStop : props.onSend}
          style={[styles.send, { backgroundColor: colors.accent }, !stopping && !canSend && styles.dim]}
        >
          {stopping ? <Square size={14} color={colors.accentFg} fill={colors.accentFg} /> : <ArrowUp size={20} color={colors.accentFg} />}
        </Pressable>
      </View>

      <Sheet open={picking === 'model'} onClose={() => setPicking(null)} title={t('chat.model.pick')}>
        {props.models.length === 0 ? <SheetOption label={t('chat.model.none')} disabled onPress={() => undefined} /> : null}
        {props.models.map((m) => (
          <SheetOption
            key={m.id}
            label={m.name}
            detail={m.atCapacity ? t('chat.model.at-capacity') : undefined}
            disabled={m.atCapacity}
            selected={m.id === props.modelId}
            right={m.id === props.modelId ? <Check size={18} color={colors.text} /> : null}
            onPress={() => {
              props.onPickModel(m.id)
              setPicking(null)
            }}
          />
        ))}
      </Sheet>
      <Sheet open={picking === 'effort'} onClose={() => setPicking(null)} title={t('chat.thinking.heading')}>
        {(['auto', ...props.thinkingLevels] as ReasoningEffort[]).map((level) => (
          <SheetOption
            key={level}
            label={t(`chat.thinking.${level}`)}
            selected={level === props.effort}
            right={level === props.effort ? <Check size={18} color={colors.text} /> : null}
            onPress={() => {
              props.onPickEffort(level)
              setPicking(null)
            }}
          />
        ))}
      </Sheet>
    </View>
  )
}

function Chip({ label, icon, chevron, active, onPress }: { label: string; icon?: React.ReactNode; chevron?: boolean; active?: boolean; onPress: () => void }) {
  const colors = useColors()
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityState={{ selected: active }}
      style={[styles.chip, { backgroundColor: active ? colors.accent : colors.field }]}
    >
      {icon}
      <Text style={[styles.chipText, { color: active ? colors.accentFg : colors.text2 }]} numberOfLines={1}>
        {label}
      </Text>
      {chevron ? <ChevronDown size={14} color={colors.text3} /> : null}
    </Pressable>
  )
}

const styles = StyleSheet.create({
  box: { borderRadius: 24, paddingHorizontal: 14, paddingTop: 10, paddingBottom: 10, gap: 8 },
  revise: { fontSize: 12, lineHeight: 17 },
  input: { fontSize: 16, lineHeight: 22, maxHeight: 160, minHeight: 24, paddingVertical: 4, textAlignVertical: 'top' },
  row: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  spacer: { flex: 1 },
  chip: { flexDirection: 'row', alignItems: 'center', gap: 4, height: 32, paddingHorizontal: 10, borderRadius: 16, maxWidth: 150 },
  chipText: { fontSize: 13, flexShrink: 1 },
  send: { width: 36, height: 36, borderRadius: 18, alignItems: 'center', justifyContent: 'center' },
  dim: { opacity: 0.3 },
})
