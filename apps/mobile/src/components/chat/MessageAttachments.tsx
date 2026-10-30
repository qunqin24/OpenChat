import type { Attachment, ConversationReference, RemovedAttachment } from '@openchat/api-client'
import { useQuery } from '@tanstack/react-query'
import { downloadAsync } from 'expo-file-system/legacy'
import { router } from 'expo-router'
import * as Sharing from 'expo-sharing'
import { FileText, FileX, MessagesSquare } from 'lucide-react-native'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ActivityIndicator, Image, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'

import { formatSize, opensInPanel, TEXT_TYPE } from '@openchat/chat-core/attachments'
import { displayTitle } from '@openchat/chat-core/titles'

import { Button } from '@/components/Button'
import { Sheet } from '@/components/Sheet'
import { api, unwrap } from '@/lib/api'
import { toast } from '@/lib/toast'
import { cachePath, onServer } from '@/lib/upload'
import { useColors } from '@/theme'
import { useViewer } from './genui/pictures'

/**
 * What a message carried, under its bubble (the web's MessageAttachments):
 * pictures as thumbnails that open full screen; other files as chips - a
 * document's words open in a sheet, anything else is fetched and handed to the
 * system's "open with"; files deleted since as a muted chip; and the
 * conversations it refers to, each a way to that conversation. Links are
 * signed and expire, so a picture that no longer loads becomes its chip.
 */
export function MessageAttachments({
  items,
  removed = [],
  references = [],
}: {
  items: Attachment[]
  removed?: RemovedAttachment[]
  references?: ConversationReference[]
}) {
  const { t } = useTranslation()
  const colors = useColors()
  const { viewer, show } = useViewer()
  const [opening, setOpening] = useState<Attachment | null>(null)
  const [broken, setBroken] = useState<string[]>([])
  if (items.length === 0 && removed.length === 0 && references.length === 0) return null

  return (
    <View style={styles.files}>
      {items.map((item) =>
        item.contentType.startsWith('image/') && item.url && !broken.includes(item.id) ? (
          <Pressable
            key={item.id}
            onPress={() => show({ uri: source(item.url), found: null }, item.filename)}
            accessibilityLabel={t('chat.file.open', { name: item.filename })}
            style={[styles.picture, { backgroundColor: colors.bubble }]}
          >
            <Image source={{ uri: source(item.url) }} style={StyleSheet.absoluteFill} onError={() => setBroken((b) => [...b, item.id])} />
          </Pressable>
        ) : (
          <Pressable
            key={item.id}
            onPress={() => (opensInPanel(item.contentType) ? setOpening(item) : void openElsewhere(item, t('chat.file.load-failed')))}
            accessibilityLabel={t('chat.file.open', { name: item.filename })}
            style={[styles.chip, { backgroundColor: colors.card, borderColor: colors.line }]}
          >
            <FileText size={18} color={colors.text2} />
            <View style={styles.chipBody}>
              <Text style={[styles.name, { color: colors.text }]} numberOfLines={1}>
                {item.filename}
              </Text>
              <Text style={[styles.meta, { color: colors.text3 }]}>{formatSize(item.size)}</Text>
            </View>
          </Pressable>
        ),
      )}
      {removed.map((item) => (
        <View key={item.id} style={[styles.chip, styles.removed, { borderColor: colors.line }]}>
          <FileX size={18} color={colors.text3} />
          <View style={styles.chipBody}>
            <Text style={[styles.name, { color: colors.text3 }]} numberOfLines={1}>
              {item.filename}
            </Text>
            <Text style={[styles.meta, { color: colors.text3 }]}>{t('chat.file.removed')}</Text>
          </View>
        </View>
      ))}
      {references.map((reference) => (
        <Pressable
          key={reference.conversationId}
          onPress={() => router.setParams({ id: reference.conversationId, n: undefined })}
          accessibilityLabel={t('chat.reference.open', { title: displayTitle(reference.title, t) })}
          style={[styles.chip, { backgroundColor: colors.card, borderColor: colors.line }]}
        >
          <MessagesSquare size={18} color={colors.text2} />
          <View style={styles.chipBody}>
            <Text style={[styles.name, { color: colors.text }]} numberOfLines={1}>
              {displayTitle(reference.title, t)}
            </Text>
            <Text style={[styles.meta, { color: colors.text3 }]}>{t('chat.reference.kind')}</Text>
          </View>
        </Pressable>
      ))}
      {opening ? <FileSheet item={opening} onClose={() => setOpening(null)} /> : null}
      {viewer}
    </View>
  )
}

/** A file's address: on the device while its message is on its way, else the server's signed link. */
const source = (url: string) => (/^(file|content):/i.test(url) ? url : onServer(url))

/** Fetches the file and hands it to the system, which offers the apps that open it. */
async function openElsewhere(item: Attachment, failed: string) {
  try {
    const link = unwrap(await api.GET('/files/{fileId}/download', { params: { path: { fileId: item.id } } }))
    const target = cachePath(item.id, item.filename)
    const { uri, status } = await downloadAsync(onServer(link.url), target)
    if (status < 200 || status >= 300) throw new Error(`file ${status}`)
    await Sharing.shareAsync(uri, { mimeType: item.contentType, dialogTitle: item.filename })
  } catch {
    toast.error(failed)
  }
}

/** A document's words, as the server reads them, with a way to open the file itself. */
function FileSheet({ item, onClose }: { item: Attachment; onClose: () => void }) {
  const { t } = useTranslation()
  const colors = useColors()
  const text = useQuery({
    queryKey: ['file-text', item.id],
    queryFn: async ({ signal }): Promise<string> => {
      // A text file is its own words; a Word, Excel or PDF file's words are read by the server.
      if (item.contentType !== TEXT_TYPE) {
        return unwrap(await api.GET('/files/{fileId}/text', { params: { path: { fileId: item.id } }, parseAs: 'text', signal })) as string
      }
      const link = unwrap(await api.GET('/files/{fileId}/download', { params: { path: { fileId: item.id } }, signal }))
      const response = await fetch(onServer(link.url), { signal })
      if (!response.ok) throw new Error(`file ${response.status}`)
      return response.text()
    },
    staleTime: Infinity,
  })
  return (
    <Sheet open onClose={onClose} title={item.filename}>
      <View style={styles.sheet}>
        {text.isPending ? (
          <ActivityIndicator color={colors.text3} />
        ) : text.isError ? (
          <Text style={[styles.meta, { color: colors.errInk }]}>{t('chat.file.load-failed')}</Text>
        ) : (
          <ScrollView style={[styles.text, { backgroundColor: colors.code }]} nestedScrollEnabled>
            <Text style={[styles.words, { color: colors.text }]} selectable>
              {text.data}
            </Text>
          </ScrollView>
        )}
        <Button kind="secondary" label={t('chat.file.download')} onPress={() => void openElsewhere(item, t('chat.file.load-failed'))} />
      </View>
    </Sheet>
  )
}

const styles = StyleSheet.create({
  files: { flexDirection: 'row', flexWrap: 'wrap', justifyContent: 'flex-end', gap: 6, maxWidth: '85%' },
  picture: { width: 120, height: 120, borderRadius: 14, overflow: 'hidden' },
  chip: { flexDirection: 'row', alignItems: 'center', gap: 8, maxWidth: 240, minWidth: 160, paddingHorizontal: 10, paddingVertical: 8, borderRadius: 12, borderWidth: StyleSheet.hairlineWidth },
  removed: { opacity: 0.7 },
  chipBody: { flexShrink: 1, gap: 1 },
  name: { fontSize: 13 },
  meta: { fontSize: 11 },
  sheet: { gap: 12, paddingHorizontal: 8, paddingBottom: 8 },
  text: { maxHeight: 380, borderRadius: 12 },
  words: { fontFamily: 'monospace', fontSize: 13, lineHeight: 20, padding: 12 },
})
