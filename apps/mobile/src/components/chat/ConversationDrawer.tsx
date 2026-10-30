import type { Conversation } from '@openchat/api-client'
import * as Haptics from 'expo-haptics'
import { AlarmClock, Pin, Search, SquarePen, X } from 'lucide-react-native'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, Pressable, SectionList, StyleSheet, Text, TextInput, View } from 'react-native'
import { KeyboardAvoidingView } from 'react-native-keyboard-controller'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { groupConversations } from '@openchat/chat-core/grouping'
import { displayTitle } from '@openchat/chat-core/titles'

import { HeaderIcon, HeaderPill } from '@/components/HeaderPill'
import { Sheet, SheetOption } from '@/components/Sheet'
import { UserAvatar } from '@/components/UserAvatar'
import {
  useConversations,
  useConversationSearch,
  useDeleteConversation,
  usePinnedConversations,
  useRunningConversations,
  useUpdateConversation,
} from '@/hooks/conversations'
import { useChatFeatures } from '@/hooks/models'
import { useUser } from '@/lib/session'
import { toast } from '@/lib/toast'
import { useColors } from '@/theme'
import { Pulse } from './Pulse'

interface Props {
  /** The conversation on screen, marked in the list. */
  current?: string
  onOpen: (conversationId: string) => void
  onNewChat: () => void
  /** The scheduled tasks screen; absent where an administrator has them off. */
  onTasks: () => void
  onSettings: () => void
}

/**
 * The conversation list in the drawer (the web's Sidebar), laid out as Kimi's
 * app has it: who is signed in at the top (the way to settings) beside a new
 * chat, the conversations - pinned first, then by when - and at the foot,
 * where the thumb is, a search over titles and what was said, and the
 * scheduled tasks. A long press offers pin, rename and delete.
 */
export function ConversationDrawer({ current, onOpen, onNewChat, onTasks, onSettings }: Props) {
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
  const features = useChatFeatures()

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

  const tasksOn = features.data?.scheduledTasks !== false
  return (
    // The search is at the foot: the drawer rises with the keyboard so it stays in sight.
    <KeyboardAvoidingView behavior="padding" style={[styles.drawer, { backgroundColor: colors.panel, paddingTop: insets.top + 10 }]}>
      {/* Who is signed in (the way to settings), and a new chat. */}
      <View style={styles.top}>
        <Pressable onPress={onSettings} accessibilityRole="button" accessibilityLabel={t('settings.title')} style={styles.who}>
          {user ? <UserAvatar id={user.id} name={user.name} email={user.email} src={user.avatarUrl} size={44} /> : null}
          <Text style={[styles.user, { color: colors.text }]} numberOfLines={1}>
            {user?.name}
          </Text>
        </Pressable>
        <HeaderPill>
          <HeaderIcon onPress={onNewChat} label={t('chat.search.new-chat')}>
            <SquarePen size={20} color={colors.text} />
          </HeaderIcon>
        </HeaderPill>
      </View>

      <SectionList
        sections={sections}
        keyExtractor={(item) => item.id}
        stickySectionHeadersEnabled={false}
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={styles.list}
        renderSectionHeader={({ section }) =>
          section.title ? <Text style={[styles.section, { color: colors.text3 }]}>{section.title}</Text> : null
        }
        renderItem={({ item }) => {
          const selected = item.id === current
          return (
            <Pressable
              onPress={() => onOpen(item.id)}
              onLongPress={() => {
                void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium)
                setActing(item)
              }}
              style={({ pressed }) => [styles.row, { backgroundColor: selected || pressed ? colors.line : 'transparent' }]}
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

      {/* Search where the thumb is, and the scheduled tasks beside it. */}
      <View style={[styles.bottom, { paddingBottom: insets.bottom + 10 }]}>
        <HeaderPill style={styles.searchPill}>
          <Search size={18} color={colors.text2} />
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
        </HeaderPill>
        {tasksOn ? (
          <HeaderPill>
            <HeaderIcon onPress={onTasks} label={t('tasks.title')}>
              <AlarmClock size={20} color={colors.text} />
            </HeaderIcon>
          </HeaderPill>
        ) : null}
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
    </KeyboardAvoidingView>
  )
}

const styles = StyleSheet.create({
  drawer: { flex: 1 },
  top: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingHorizontal: 16, paddingBottom: 12 },
  who: { flex: 1, flexDirection: 'row', alignItems: 'center', gap: 12 },
  user: { flexShrink: 1, fontSize: 18, fontWeight: '500' },
  list: { paddingBottom: 12 },
  section: { fontSize: 12, paddingHorizontal: 24, paddingTop: 18, paddingBottom: 6 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8, marginHorizontal: 10, paddingHorizontal: 14, paddingVertical: 13, borderRadius: 16 },
  rowTitle: { flex: 1, fontSize: 16 },
  empty: { fontSize: 14, textAlign: 'center', marginTop: 32 },
  bottom: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingHorizontal: 16, paddingTop: 8 },
  searchPill: { flex: 1, gap: 8, paddingHorizontal: 16 },
  searchInput: { flex: 1, fontSize: 16, paddingVertical: 0 },
  rename: { paddingHorizontal: 12, paddingBottom: 12 },
  renameInput: { height: 46, borderRadius: 23, paddingHorizontal: 16, fontSize: 16 },
})
