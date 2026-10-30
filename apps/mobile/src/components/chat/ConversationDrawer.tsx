import type { Conversation } from '@openchat/api-client'
import { LogOut, Pin, Search, SquarePen, X } from 'lucide-react-native'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, Pressable, SectionList, StyleSheet, Text, TextInput, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { groupConversations } from '@openchat/chat-core/grouping'
import { displayTitle } from '@openchat/chat-core/titles'

import { Sheet, SheetOption } from '@/components/Sheet'
import {
  useConversations,
  useConversationSearch,
  useDeleteConversation,
  usePinnedConversations,
  useRunningConversations,
  useUpdateConversation,
} from '@/hooks/conversations'
import { signOut, useUser } from '@/lib/session'
import { toast } from '@/lib/toast'
import { useColors } from '@/theme'
import { Pulse } from './Pulse'

interface Props {
  /** The conversation on screen, marked in the list. */
  current?: string
  onOpen: (conversationId: string) => void
  onNewChat: () => void
}

/**
 * The conversation list in the drawer (the web's Sidebar): pinned first, then
 * by when, with a search over titles and what was said. A long press offers
 * pin, rename and delete.
 */
export function ConversationDrawer({ current, onOpen, onNewChat }: Props) {
  const { t, i18n } = useTranslation()
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const user = useUser()
  const [term, setTerm] = useState('')
  const [acting, setActing] = useState<Conversation | null>(null)
  const [renaming, setRenaming] = useState<{ conversation: Conversation; title: string } | null>(null)

  const list = useConversations()
  const pinned = usePinnedConversations()
  const search = useConversationSearch(term.trim())
  const running = useRunningConversations()
  const update = useUpdateConversation()
  const remove = useDeleteConversation()

  const sections = useMemo(() => {
    if (term.trim()) {
      const found = search.data?.pages.flatMap((page) => page.items) ?? []
      return found.length ? [{ key: 'search', title: '', data: found }] : []
    }
    const pinnedItems = pinned.data ?? []
    const pinnedIds = new Set(pinnedItems.map((c) => c.id))
    const history = (list.data?.pages.flatMap((page) => page.items) ?? []).filter((c) => !pinnedIds.has(c.id))
    const groups = groupConversations(history, t, i18n.language).map((g) => ({ key: g.key, title: g.label, data: g.items }))
    return pinnedItems.length ? [{ key: 'pinned', title: t('chat.group.pinned'), data: pinnedItems }, ...groups] : groups
  }, [term, search.data, pinned.data, list.data, t, i18n.language])

  const confirmDelete = (conversation: Conversation) => {
    Alert.alert(
      t('chat.conversation.remove-title'),
      t('chat.conversation.remove-description', { title: displayTitle(conversation.title, t) }),
      [
        { text: t('chat.conversation.cancel'), style: 'cancel' },
        {
          text: t('chat.delete'),
          style: 'destructive',
          onPress: () =>
            remove.mutate(conversation.id, {
              onSuccess: () => {
                if (conversation.id === current) onNewChat()
              },
              onError: () => toast.error(t('chat.conversation.remove-failed')),
            }),
        },
      ],
    )
  }

  return (
    <View style={[styles.drawer, { backgroundColor: colors.bg, paddingTop: insets.top + 8, paddingBottom: insets.bottom + 8 }]}>
      <View style={styles.top}>
        <View style={[styles.search, { backgroundColor: colors.field }]}>
          <Search size={16} color={colors.text3} />
          <TextInput
            style={[styles.searchInput, { color: colors.text }]}
            placeholder={t('chat.sidebar.search')}
            placeholderTextColor={colors.text3}
            value={term}
            onChangeText={setTerm}
            returnKeyType="search"
          />
          {term ? (
            <Pressable hitSlop={8} onPress={() => setTerm('')} accessibilityLabel={t('chat.search.clear')}>
              <X size={16} color={colors.text3} />
            </Pressable>
          ) : null}
        </View>
        <Pressable hitSlop={8} style={styles.iconButton} onPress={onNewChat} accessibilityLabel={t('chat.search.new-chat')}>
          <SquarePen size={21} color={colors.text} />
        </Pressable>
      </View>

      <SectionList
        sections={sections}
        keyExtractor={(item) => item.id}
        stickySectionHeadersEnabled={false}
        keyboardShouldPersistTaps="handled"
        renderSectionHeader={({ section }) =>
          section.title ? <Text style={[styles.section, { color: colors.text3 }]}>{section.title}</Text> : null
        }
        renderItem={({ item }) => {
          const selected = item.id === current
          return (
            <Pressable
              onPress={() => onOpen(item.id)}
              onLongPress={() => setActing(item)}
              style={({ pressed }) => [styles.row, { backgroundColor: selected || pressed ? colors.field : 'transparent' }]}
            >
              <Text style={[styles.rowTitle, { color: colors.text }]} numberOfLines={1}>
                {displayTitle(item.title, t)}
              </Text>
              {running.has(item.id) ? <Pulse size={6} /> : item.pinnedAt && term ? <Pin size={13} color={colors.text3} /> : null}
            </Pressable>
          )
        }}
        ListEmptyComponent={
          list.isPending ? undefined : (
            <Text style={[styles.empty, { color: colors.text3 }]}>{term ? t('chat.sidebar.search-empty') : t('chat.conversation.empty')}</Text>
          )
        }
        onEndReached={() => {
          const query = term.trim() ? search : list
          if (query.hasNextPage && !query.isFetchingNextPage) void query.fetchNextPage()
        }}
        onEndReachedThreshold={0.4}
      />

      <View style={[styles.footer, { borderTopColor: colors.line }]}>
        <Text style={[styles.user, { color: colors.text }]} numberOfLines={1}>
          {user?.name}
        </Text>
        <Pressable hitSlop={8} style={styles.iconButton} onPress={() => void signOut()} accessibilityLabel={t('auth.logout')}>
          <LogOut size={19} color={colors.text2} />
        </Pressable>
      </View>

      <Sheet open={acting !== null} onClose={() => setActing(null)} title={acting ? displayTitle(acting.title, t) : undefined}>
        {acting ? (
          <>
            <SheetOption
              label={acting.pinnedAt ? t('chat.unpin') : t('chat.pin')}
              onPress={() => {
                update.mutate(
                  { conversationId: acting.id, pinned: !acting.pinnedAt },
                  { onError: () => toast.error(acting.pinnedAt ? t('chat.unpin-failed') : t('chat.pin-failed')) },
                )
                setActing(null)
              }}
            />
            <SheetOption
              label={t('chat.rename')}
              onPress={() => {
                setRenaming({ conversation: acting, title: acting.title ?? '' })
                setActing(null)
              }}
            />
            <SheetOption
              label={t('chat.delete')}
              onPress={() => {
                const conversation = acting
                setActing(null)
                confirmDelete(conversation)
              }}
            />
          </>
        ) : null}
      </Sheet>

      <Sheet open={renaming !== null} onClose={() => setRenaming(null)} title={t('chat.rename')}>
        {renaming ? (
          <View style={styles.rename}>
            <TextInput
              style={[styles.renameInput, { color: colors.text, backgroundColor: colors.field }]}
              value={renaming.title}
              onChangeText={(title) => setRenaming({ ...renaming, title })}
              autoFocus
              returnKeyType="done"
              onSubmitEditing={() => {
                const title = renaming.title.trim()
                if (title) update.mutate({ conversationId: renaming.conversation.id, title })
                setRenaming(null)
              }}
            />
          </View>
        ) : null}
      </Sheet>
    </View>
  )
}

const styles = StyleSheet.create({
  drawer: { flex: 1 },
  top: { flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 12, marginBottom: 8 },
  search: { flex: 1, flexDirection: 'row', alignItems: 'center', gap: 8, height: 40, borderRadius: 20, paddingHorizontal: 12 },
  searchInput: { flex: 1, fontSize: 15, paddingVertical: 0 },
  iconButton: { padding: 8 },
  section: { fontSize: 12, paddingHorizontal: 20, paddingTop: 16, paddingBottom: 6 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8, marginHorizontal: 8, paddingHorizontal: 12, paddingVertical: 11, borderRadius: 12 },
  rowTitle: { flex: 1, fontSize: 15 },
  empty: { fontSize: 14, textAlign: 'center', marginTop: 32 },
  footer: { flexDirection: 'row', alignItems: 'center', paddingHorizontal: 20, paddingTop: 8, borderTopWidth: StyleSheet.hairlineWidth },
  user: { flex: 1, fontSize: 15, fontWeight: '500' },
  rename: { paddingHorizontal: 12, paddingBottom: 12 },
  renameInput: { height: 46, borderRadius: 23, paddingHorizontal: 16, fontSize: 16 },
})
