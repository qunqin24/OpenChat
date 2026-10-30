import type { ConversationReference } from '@openchat/api-client'
import { FileText, MessagesSquare, X } from 'lucide-react-native'
import { useTranslation } from 'react-i18next'
import { Image, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'

import { formatSize } from '@openchat/chat-core/attachments'
import { displayTitle } from '@openchat/chat-core/titles'

import type { PendingAttachment } from '@/hooks/attachments'
import { useColors } from '@/theme'

/**
 * What goes with the message being written, above the box (the web's
 * AttachmentTray): pictures as thumbnails, other files as a chip with their
 * size, and the conversations it refers to; each with how its upload stands
 * and a way to take it out.
 */
export function AttachmentTray({
  items,
  references,
  onRemove,
  onUnrefer,
}: {
  items: PendingAttachment[]
  references: ConversationReference[]
  onRemove: (key: string) => void
  onUnrefer: (conversationId: string) => void
}) {
  const { t } = useTranslation()
  const colors = useColors()
  if (items.length === 0 && references.length === 0) return null
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.tray} keyboardShouldPersistTaps="handled">
      {items.map((item) => {
        const state =
          item.status === 'error'
            ? t(`chat.file.error.${item.error ?? 'failed'}`)
            : item.status === 'ready'
              ? formatSize(item.size)
              : t('chat.file.uploading', { percent: Math.round(item.progress * 100) })
        return (
          <View
            key={item.key}
            style={[
              item.preview ? styles.thumb : styles.chip,
              { backgroundColor: colors.card, borderColor: item.status === 'error' ? colors.errInk : colors.line },
            ]}
          >
            {item.preview ? (
              <Image source={{ uri: item.preview }} style={[StyleSheet.absoluteFill, item.status !== 'ready' && styles.dim]} />
            ) : (
              <>
                <FileText size={18} color={colors.text2} />
                <View style={styles.chipBody}>
                  <Text style={[styles.name, { color: colors.text }]} numberOfLines={1}>
                    {item.name}
                  </Text>
                  <Text style={[styles.meta, { color: item.status === 'error' ? colors.errInk : colors.text3 }]}>{state}</Text>
                </View>
              </>
            )}
            {item.preview && item.status !== 'ready' ? (
              <Text style={[styles.overlay, { color: item.status === 'error' ? colors.errInk : colors.text }]}>{state}</Text>
            ) : null}
            <Pressable
              hitSlop={8}
              onPress={() => onRemove(item.key)}
              accessibilityLabel={t('chat.file.remove', { name: item.name })}
              style={[styles.remove, { backgroundColor: colors.accent }]}
            >
              <X size={12} color={colors.accentFg} />
            </Pressable>
          </View>
        )
      })}
      {references.map((reference) => (
        <View key={reference.conversationId} style={[styles.chip, { backgroundColor: colors.card, borderColor: colors.line }]}>
          <MessagesSquare size={18} color={colors.text2} />
          <View style={styles.chipBody}>
            <Text style={[styles.name, { color: colors.text }]} numberOfLines={1}>
              {displayTitle(reference.title, t)}
            </Text>
            <Text style={[styles.meta, { color: colors.text3 }]}>{t('chat.reference.kind')}</Text>
          </View>
          <Pressable
            hitSlop={8}
            onPress={() => onUnrefer(reference.conversationId)}
            accessibilityLabel={t('chat.file.remove', { name: displayTitle(reference.title, t) })}
            style={[styles.remove, { backgroundColor: colors.accent }]}
          >
            <X size={12} color={colors.accentFg} />
          </Pressable>
        </View>
      ))}
    </ScrollView>
  )
}

const styles = StyleSheet.create({
  tray: { gap: 8, paddingTop: 2, paddingRight: 6 },
  thumb: { width: 64, height: 64, borderRadius: 12, borderWidth: StyleSheet.hairlineWidth, overflow: 'hidden', justifyContent: 'flex-end' },
  dim: { opacity: 0.45 },
  overlay: { fontSize: 10, textAlign: 'center', paddingBottom: 4, fontWeight: '600' },
  chip: { flexDirection: 'row', alignItems: 'center', gap: 8, width: 200, height: 56, paddingLeft: 10, paddingRight: 26, borderRadius: 12, borderWidth: StyleSheet.hairlineWidth },
  chipBody: { flex: 1, gap: 1 },
  name: { fontSize: 13 },
  meta: { fontSize: 11 },
  remove: { position: 'absolute', top: 4, right: 4, width: 18, height: 18, borderRadius: 9, alignItems: 'center', justifyContent: 'center' },
})
