import type { ConversationReference, LibraryFile } from '@openchat/api-client'
import { useInfiniteQuery } from '@tanstack/react-query'
import * as DocumentPicker from 'expo-document-picker'
import { downloadAsync } from 'expo-file-system/legacy'
import * as ImagePicker from 'expo-image-picker'
import { Camera, Check, ChevronLeft, ChevronRight, FileUp, Globe, Image as ImageIcon, LibraryBig, MessagesSquare, type LucideIcon } from 'lucide-react-native'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ActivityIndicator, FlatList, Pressable, StyleSheet, Text, TextInput, View } from 'react-native'

import { queryKeys } from '@openchat/chat-core/query-keys'
import { displayTitle } from '@openchat/chat-core/titles'

import { Sheet } from '@/components/Sheet'
import { useConversations, useConversationSearch } from '@/hooks/conversations'
import { api, unwrap } from '@/lib/api'
import { toast } from '@/lib/toast'
import { cachePath, onServer, type LocalFile } from '@/lib/upload'
import { Toggle } from '@/components/Toggle'
import { useColors } from '@/theme'

export type PickedFile = LocalFile & { width?: number; height?: number }

interface Props {
  open: boolean
  onClose: () => void
  /** Files to attach: photos, a photo just taken, files, or files sent before. */
  onFiles: (files: PickedFile[]) => void
  /** Whether a picked kind can go at all, for the photos and camera items. */
  canImages: boolean
  /** Another conversation to refer to; absent where none can be (a refused message being reworded). */
  onRefer?: (reference: ConversationReference) => void
  /** The conversations already referred to, and the one on screen (never offered). */
  referred: string[]
  current?: string
  /** Whether the model may go on the web; absent where it cannot (the feature is off, or the model takes no tools). */
  web?: { on: boolean; onToggle: () => void }
}

type View_ = 'main' | 'library' | 'conversations'

/**
 * What can go with a message (the web's AttachMenu): photos through the
 * system's picker, a photo taken now, any file, a file sent before (the
 * library), or another conversation to refer to.
 */
export function AttachMenu({ open, onClose, onFiles, canImages, onRefer, referred, current, web }: Props) {
  const { t } = useTranslation()
  const [view, setView] = useState<View_>('main')
  const close = () => {
    setView('main')
    onClose()
  }
  // The pickers open over the app: the sheet goes first, so it is not left behind them.
  const then = (pick: () => Promise<void>) => () => {
    close()
    void pick().catch(() => toast.error(t('chat.send-error.generic')))
  }

  const photos = async () => {
    const result = await ImagePicker.launchImageLibraryAsync({ mediaTypes: ['images'], allowsMultipleSelection: true, selectionLimit: 10, quality: 1 })
    if (!result.canceled) onFiles(result.assets.map(fromImage))
  }
  const camera = async () => {
    const result = await ImagePicker.launchCameraAsync({ mediaTypes: ['images'], quality: 0.9 })
    if (!result.canceled) onFiles(result.assets.map(fromImage))
  }
  const files = async () => {
    const result = await DocumentPicker.getDocumentAsync({ multiple: true, copyToCacheDirectory: true })
    if (!result.canceled) {
      onFiles(result.assets.map((asset) => ({ uri: asset.uri, name: asset.name, type: asset.mimeType ?? '', size: asset.size ?? 0 })))
    }
  }
  const fromLibrary = async (file: LibraryFile) => {
    close()
    toast.info(t('chat.attach.loading', { name: file.filename }))
    try {
      // Fetched and sent again, as the web does: an upload belongs to one message.
      const target = cachePath(`library-${file.id}`, file.filename)
      const { uri, status } = await downloadAsync(onServer(file.url), target)
      if (status < 200 || status >= 300) throw new Error(`file ${status}`)
      onFiles([{ uri, name: file.filename, type: file.contentType, size: file.size }])
    } catch {
      toast.error(t('chat.attach.failed', { name: file.filename }))
    }
  }

  return (
    <Sheet open={open} onClose={close}>
      {view === 'main' ? (
        <View style={styles.main}>
          {/* What comes from the phone, a tap each; a model that cannot see pictures leaves their tiles dim. */}
          <View style={styles.tiles}>
            <Tile Icon={Camera} label={t('mobile.attach-camera')} onPress={then(camera)} disabled={!canImages} />
            <Tile Icon={ImageIcon} label={t('mobile.attach-photos')} onPress={then(photos)} disabled={!canImages} />
            <Tile Icon={FileUp} label={t('mobile.attach-files')} onPress={then(files)} />
          </View>
          <Item Icon={LibraryBig} label={t('chat.attach.library-title')} detail={t('mobile.attach-library-hint')} onPress={() => setView('library')} />
          {onRefer ? (
            <Item Icon={MessagesSquare} label={t('chat.attach.conversation')} detail={t('mobile.attach-conversation-hint')} onPress={() => setView('conversations')} />
          ) : null}
          {/* A setting, not a way in: it switches where it is and the menu stays open. */}
          {web ? (
            <Item
              Icon={Globe}
              label={t('chat.web-search.label')}
              detail={t('mobile.attach-web-hint')}
              onPress={web.onToggle}
              right={<Toggle value={web.on} onValueChange={web.onToggle} />}
            />
          ) : null}
        </View>
      ) : view === 'library' ? (
        <LibraryChoices onBack={() => setView('main')} onPick={(file) => void fromLibrary(file)} />
      ) : (
        <ConversationChoices
          onBack={() => setView('main')}
          exclude={current}
          chosen={referred}
          onPick={(reference) => {
            onRefer?.(reference)
            close()
          }}
        />
      )}
    </Sheet>
  )
}

const fromImage = (asset: ImagePicker.ImagePickerAsset): PickedFile => ({
  uri: asset.uri,
  name: asset.fileName ?? `photo-${Date.now()}.jpg`,
  type: asset.mimeType ?? 'image/jpeg',
  size: asset.fileSize ?? 0,
  width: asset.width,
  height: asset.height,
})

/** One of the row of square tiles: an icon over its name. */
function Tile({ Icon, label, onPress, disabled }: { Icon: LucideIcon; label: string; onPress: () => void; disabled?: boolean }) {
  const colors = useColors()
  return (
    <Pressable
      onPress={onPress}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityState={{ disabled }}
      style={({ pressed }) => [styles.tile, { backgroundColor: pressed ? colors.field : colors.bubble }, disabled && styles.dim]}
    >
      <Icon size={24} color={colors.text} strokeWidth={1.75} />
      <Text style={[styles.tileLabel, { color: colors.text }]} numberOfLines={1}>
        {label}
      </Text>
    </Pressable>
  )
}

/** A way into a list of its own: what it is, a line on what it is for, and the way in. */
function Item({ Icon, label, detail, onPress, right }: { Icon: LucideIcon; label: string; detail: string; onPress: () => void; right?: ReactNode }) {
  const colors = useColors()
  return (
    <Pressable onPress={onPress} style={({ pressed }) => [styles.item, pressed && { backgroundColor: colors.field }]} accessibilityRole="button">
      <Icon size={22} color={colors.text} strokeWidth={1.75} />
      <View style={styles.itemText}>
        <Text style={[styles.label, { color: colors.text }]}>{label}</Text>
        <Text style={[styles.detail, { color: colors.text3 }]} numberOfLines={1}>
          {detail}
        </Text>
      </View>
      {right ?? <ChevronRight size={18} color={colors.text3} />}
    </Pressable>
  )
}

function Panel({ title, placeholder, typed, onType, onBack, children }: { title: string; placeholder: string; typed: string; onType: (value: string) => void; onBack: () => void; children: ReactNode }) {
  const { t } = useTranslation()
  const colors = useColors()
  return (
    <View style={styles.panel}>
      <View style={styles.panelHead}>
        <Pressable hitSlop={10} onPress={onBack} accessibilityLabel={t('chat.attach.back')}>
          <ChevronLeft size={22} color={colors.text} />
        </Pressable>
        <Text style={[styles.panelTitle, { color: colors.text }]}>{title}</Text>
      </View>
      <TextInput
        style={[styles.search, { color: colors.text, backgroundColor: colors.field }]}
        placeholder={placeholder}
        placeholderTextColor={colors.text3}
        value={typed}
        onChangeText={onType}
      />
      {children}
    </View>
  )
}

function LibraryChoices({ onBack, onPick }: { onBack: () => void; onPick: (file: LibraryFile) => void }) {
  const { t } = useTranslation()
  const colors = useColors()
  const [typed, setTyped] = useState('')
  const search = typed.trim()
  const library = useInfiniteQuery({
    queryKey: queryKeys.library.list(search, 'all'),
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ signal, pageParam }) =>
      unwrap(await api.GET('/files', { params: { query: { limit: 60, cursor: pageParam, ...(search ? { search } : {}) } }, signal })),
    getNextPageParam: (page) => page.nextCursor ?? undefined,
    placeholderData: (previous) => previous,
    staleTime: 0,
  })
  const files = (library.data?.pages ?? []).flatMap((page) => page.items)
  return (
    <Panel title={t('chat.attach.library-title')} placeholder={t('chat.attach.library-search')} typed={typed} onType={setTyped} onBack={onBack}>
      {library.isPending ? (
        <ActivityIndicator style={styles.note} color={colors.text3} />
      ) : files.length === 0 ? (
        <Text style={[styles.note, { color: colors.text3 }]}>{search ? t('chat.attach.no-file-match') : t('chat.attach.library-empty')}</Text>
      ) : (
        <FlatList
          data={files}
          keyExtractor={(file) => file.id}
          style={styles.list}
          onEndReached={() => library.hasNextPage && !library.isFetchingNextPage && void library.fetchNextPage()}
          renderItem={({ item }) => (
            <Pressable onPress={() => onPick(item)} style={({ pressed }) => [styles.row, pressed && { backgroundColor: colors.field }]}>
              <Text style={[styles.rowText, { color: colors.text }]} numberOfLines={1}>
                {item.filename}
              </Text>
            </Pressable>
          )}
        />
      )}
    </Panel>
  )
}

function ConversationChoices({ onBack, exclude, chosen, onPick }: { onBack: () => void; exclude?: string; chosen: string[]; onPick: (reference: ConversationReference) => void }) {
  const { t } = useTranslation()
  const colors = useColors()
  const [typed, setTyped] = useState('')
  const term = typed.trim()
  const latest = useConversations()
  const search = useConversationSearch(term)
  const list = term ? search : latest
  const conversations = (list.data?.pages ?? []).flatMap((page) => page.items).filter((c) => c.id !== exclude)
  return (
    <Panel title={t('chat.attach.conversations-title')} placeholder={t('chat.attach.search')} typed={typed} onType={setTyped} onBack={onBack}>
      {list.isPending ? (
        <ActivityIndicator style={styles.note} color={colors.text3} />
      ) : conversations.length === 0 ? (
        <Text style={[styles.note, { color: colors.text3 }]}>{term ? t('chat.attach.no-match') : t('chat.attach.conversations-empty')}</Text>
      ) : (
        <FlatList
          data={conversations}
          keyExtractor={(c) => c.id}
          style={styles.list}
          onEndReached={() => list.hasNextPage && !list.isFetchingNextPage && void list.fetchNextPage()}
          renderItem={({ item }) => {
            const isChosen = chosen.includes(item.id)
            return (
              <Pressable
                disabled={isChosen}
                onPress={() => onPick({ conversationId: item.id, title: item.title })}
                style={({ pressed }) => [styles.row, pressed && { backgroundColor: colors.field }]}
              >
                <Text style={[styles.rowText, { color: isChosen ? colors.text3 : colors.text }]} numberOfLines={1}>
                  {displayTitle(item.title, t)}
                </Text>
                {isChosen ? <Check size={16} color={colors.text3} accessibilityLabel={t('chat.attach.referred')} /> : null}
              </Pressable>
            )
          }}
        />
      )}
    </Panel>
  )
}

const styles = StyleSheet.create({
  main: { gap: 6, paddingBottom: 4 },
  tiles: { flexDirection: 'row', gap: 10, marginBottom: 8 },
  tile: { flex: 1, aspectRatio: 1, maxHeight: 104, borderRadius: 22, alignItems: 'center', justifyContent: 'center', gap: 10 },
  tileLabel: { fontSize: 14, fontWeight: '500' },
  dim: { opacity: 0.4 },
  item: { flexDirection: 'row', alignItems: 'center', gap: 16, paddingHorizontal: 12, paddingVertical: 12, borderRadius: 16 },
  itemText: { flex: 1, gap: 3 },
  label: { fontSize: 16, fontWeight: '600' },
  detail: { fontSize: 13 },
  panel: { gap: 10, paddingBottom: 8 },
  panelHead: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 8 },
  panelTitle: { fontSize: 15, fontWeight: '600' },
  search: { height: 40, borderRadius: 20, paddingHorizontal: 14, fontSize: 15, marginHorizontal: 4 },
  list: { maxHeight: 360 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 14, paddingVertical: 12, borderRadius: 12 },
  rowText: { flex: 1, fontSize: 15 },
  note: { padding: 24, textAlign: 'center', fontSize: 14 },
})
