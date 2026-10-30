import type { ChatModel } from '@openchat/api-client'
import { Check, ChevronDown } from 'lucide-react-native'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Pressable, StyleSheet, Text } from 'react-native'

import { HeaderPill, PILL } from '@/components/HeaderPill'
import { Sheet, SheetOption } from '@/components/Sheet'
import { useColors } from '@/theme'

/**
 * The model the next run answers with, as the chat's heading: its name in a
 * raised pill beside the drawer button (HeaderPill), which opens the list.
 * Kept in the header rather than the composer, as chat apps on phones do, so
 * the row under the message box holds only what changes from message to
 * message. A model at capacity stays in the list, disabled (`atCapacity`).
 */
export function ModelPicker({ models, modelId, onPick }: { models: ChatModel[]; modelId: string; onPick: (id: string) => void }) {
  const { t } = useTranslation()
  const colors = useColors()
  const [open, setOpen] = useState(false)
  const model = models.find((m) => m.id === modelId)
  return (
    <>
      <HeaderPill style={styles.shrink}>
        <Pressable
          onPress={() => setOpen(true)}
          accessibilityRole="button"
          accessibilityLabel={t('chat.model.pick')}
          style={({ pressed }) => [styles.pill, pressed && { backgroundColor: colors.field }]}
        >
          <Text style={[styles.name, { color: colors.text }]} numberOfLines={1}>
            {model?.name ?? (modelId || t('chat.model.pick'))}
          </Text>
          <ChevronDown size={15} color={colors.text3} />
        </Pressable>
      </HeaderPill>
      <Sheet open={open} onClose={() => setOpen(false)} title={t('chat.model.pick')}>
        {models.length === 0 ? <SheetOption label={t('chat.model.none')} disabled onPress={() => undefined} /> : null}
        {models.map((m) => (
          <SheetOption
            key={m.id}
            label={m.name}
            detail={m.atCapacity ? t('chat.model.at-capacity') : undefined}
            disabled={m.atCapacity}
            selected={m.id === modelId}
            right={m.id === modelId ? <Check size={18} color={colors.text} /> : null}
            onPress={() => {
              onPick(m.id)
              setOpen(false)
            }}
          />
        ))}
      </Sheet>
    </>
  )
}

const styles = StyleSheet.create({
  shrink: { flexShrink: 1 },
  pill: { flexDirection: 'row', alignItems: 'center', gap: 4, flexShrink: 1, height: PILL - 2, paddingHorizontal: 16, borderRadius: PILL / 2 },
  name: { fontSize: 16, fontWeight: '500', flexShrink: 1 },
})
