import type { Memory, MemoryEvent } from '@openchat/api-client'
import { Brain, ChevronDown, ChevronRight, Plus, Search, Trash2, X } from 'lucide-react-native'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ActivityIndicator, Alert, Modal, Pressable, StyleSheet, Text, TextInput, View } from 'react-native'
import { KeyboardAvoidingView } from 'react-native-keyboard-controller'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { Button } from '@/components/Button'
import { HeaderIcon } from '@/components/HeaderPill'
import { EmptyState, Group, Help, Row, SettingsPage } from '@/components/settings/parts'
import {
  useClearMemories,
  useCreateMemory,
  useDeleteMemory,
  useMemories,
  useMemoryErrorText,
  useMemoryEvents,
  usePersonalization,
  useUndoErrorText,
  useUndoMemoryEvent,
  useUpdateMemory,
} from '@/hooks/memory'
import { lowerKeyboardOr } from '@/lib/back'
import { toast } from '@/lib/toast'
import { useColors } from '@/theme'

/** How many of the model's latest changes are listed. */
const RECENT = 5

/**
 * The memories (the web's MemoryManager): a search, what the model changed
 * lately with an undo, then one row per memory - tap to edit, the bin to
 * delete - and at the foot "add" and "clear".
 */
export default function MemoryScreen() {
  const { t, i18n } = useTranslation()
  const colors = useColors()
  const personalization = usePersonalization().data
  const memories = useMemories()
  const create = useCreateMemory()
  const update = useUpdateMemory()
  const remove = useDeleteMemory()
  const clear = useClearMemories()
  const errorText = useMemoryErrorText()
  const [query, setQuery] = useState('')
  // What the editor is open on: a new memory, or one being changed.
  const [editing, setEditing] = useState<{ memory?: Memory } | null>(null)

  const items = memories.data
  const limit = personalization?.memoryLimit ?? 100
  const length = personalization?.memoryLength ?? 200
  const full = (items?.length ?? 0) >= limit

  const shown = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase()
    return (items ?? [])
      .filter((m) => !needle || m.content.toLocaleLowerCase().includes(needle))
      .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))
  }, [items, query])

  const day = (iso: string) => new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(new Date(iso))

  const confirmDelete = (memory: Memory) =>
    Alert.alert(t('settings.memory.delete-confirm'), memory.content, [
      { text: t('settings.cancel'), style: 'cancel' },
      { text: t('settings.memory.delete'), style: 'destructive', onPress: () => remove.mutate(memory.id, { onError: (failure) => toast.error(errorText(failure)) }) },
    ])

  const confirmClear = () =>
    Alert.alert(t('settings.memory.clear-confirm'), undefined, [
      { text: t('settings.cancel'), style: 'cancel' },
      {
        text: t('settings.memory.clear-yes'),
        style: 'destructive',
        onPress: () =>
          clear.mutate(undefined, {
            onSuccess: () => toast.info(t('settings.memory.cleared')),
            onError: (failure) => toast.error(errorText(failure)),
          }),
      },
    ])

  return (
    <SettingsPage
      title={items ? `${t('settings.memory.title')} · ${items.length}` : t('settings.memory.title')}
      right={
        <HeaderIcon disabled={full} onPress={() => setEditing({})} label={t('settings.memory.add-memory')}>
          <Plus size={20} color={colors.text} />
        </HeaderIcon>
      }
    >
      {(items?.length ?? 0) > 0 ? (
      <View style={[styles.search, { backgroundColor: colors.field }]}>
        <Search size={16} color={colors.text3} />
        <TextInput
          style={[styles.searchInput, { color: colors.text }]}
          value={query}
          maxLength={500}
          placeholder={t('settings.memory.search')}
          placeholderTextColor={colors.text3}
          onChangeText={setQuery}
        />
        {query ? (
          <Pressable hitSlop={8} onPress={() => setQuery('')} accessibilityLabel={t('settings.memory.clear-search')}>
            <X size={16} color={colors.text3} />
          </Pressable>
        ) : null}
      </View>
      ) : null}

      <RecentChanges />

      {full ? <Help tone="warning">{t('settings.memory.full', { max: limit })}</Help> : null}

      {memories.isPending ? (
        <ActivityIndicator color={colors.text3} style={styles.spinner} />
      ) : memories.isError ? (
        <Button kind="secondary" label={t('settings.retry')} onPress={() => void memories.refetch()} />
      ) : shown.length === 0 ? (
        (items?.length ?? 0) === 0 ? (
          <EmptyState
            icon={<Brain size={26} color={colors.text2} />}
            text={t('settings.memory.empty')}
            action={<Button label={t('settings.memory.add-memory')} onPress={() => setEditing({})} />}
          />
        ) : (
          <Help>{t('settings.memory.no-match')}</Help>
        )
      ) : (
        <Group>
          {shown.map((memory) => (
            <View key={memory.id} style={styles.memory}>
              <Pressable style={styles.memoryMain} onPress={() => setEditing({ memory })} accessibilityLabel={t('settings.memory.edit')}>
                <Text style={[styles.memoryText, { color: colors.text }]}>{memory.content}</Text>
                <Text style={[styles.memoryMeta, { color: colors.text3 }]}>
                  {day(memory.updatedAt)}
                  {memory.source !== 'user' ? ` · ${t(`settings.memory.source-${memory.source}`)}` : ''}
                </Text>
              </Pressable>
              <Pressable hitSlop={10} onPress={() => confirmDelete(memory)} accessibilityLabel={t('settings.memory.delete')}>
                <Trash2 size={17} color={colors.text3} />
              </Pressable>
            </View>
          ))}
        </Group>
      )}

      {(items?.length ?? 0) > 0 ? (
        <Group>
          <Row label={t('settings.memory.clear-memory')} danger chevron={false} disabled={clear.isPending} onPress={confirmClear} />
        </Group>
      ) : null}

      {editing ? (
        <MemoryEditor
          initial={editing.memory?.content ?? ''}
          mode={editing.memory ? 'edit' : 'add'}
          max={length}
          onClose={() => setEditing(null)}
          save={async (content) => {
            if (editing.memory) await update.mutateAsync({ id: editing.memory.id, content })
            else await create.mutateAsync(content)
          }}
        />
      ) : null}
    </SettingsPage>
  )
}

/** The model's latest changes, folded away until asked for, each with an undo. */
function RecentChanges() {
  const { t } = useTranslation()
  const colors = useColors()
  const events = useMemoryEvents()
  const undo = useUndoMemoryEvent()
  const errorText = useUndoErrorText()
  const [open, setOpen] = useState(false)
  const recent = (events.data ?? []).slice(0, RECENT)
  if (recent.length === 0) return null
  const Chevron = open ? ChevronDown : ChevronRight
  return (
    <Group>
      <Pressable style={styles.recentHead} onPress={() => setOpen(!open)} accessibilityRole="button" accessibilityState={{ expanded: open }}>
        <Text style={[styles.recentTitle, { color: colors.text }]}>{t('settings.memory.recent')}</Text>
        <Text style={[styles.memoryMeta, { color: colors.text3 }]}>{recent.length}</Text>
        <Chevron size={16} color={colors.text3} />
      </Pressable>
      {open
        ? recent.map((event) => (
            <View key={event.id} style={[styles.recent, event.undoneAt && styles.undone]}>
              <Text style={[styles.badge, { color: colors.text2, backgroundColor: colors.field }]}>{t(`settings.memory.op-${event.op}`)}</Text>
              <Text style={[styles.recentText, { color: colors.text }]} numberOfLines={3}>
                {changeText(event)}
              </Text>
              {event.undoneAt ? (
                <Text style={[styles.memoryMeta, { color: colors.text3 }]}>{t('settings.memory.undone')}</Text>
              ) : (
                <Pressable disabled={undo.isPending} hitSlop={6} onPress={() => undo.mutate(event, { onError: (failure) => toast.error(errorText(failure)) })}>
                  <Text style={[styles.undo, { color: colors.text }]}>{t('settings.memory.undo')}</Text>
                </Pressable>
              )}
            </View>
          ))
        : null}
    </Group>
  )
}

/** What a logged change did, in the words of the memory it touched. */
const changeText = (event: MemoryEvent) => (event.op === 'delete' ? event.before : event.after) ?? ''

/**
 * Writing one memory, full screen over the list (the web's MemoryEditor): a
 * box several lines tall with the count. What went wrong is said under it and
 * the editor stays open.
 */
function MemoryEditor({ initial, mode, max, onClose, save }: { initial: string; mode: 'add' | 'edit'; max: number; onClose: () => void; save: (content: string) => Promise<void> }) {
  const { t } = useTranslation()
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const errorText = useMemoryErrorText()
  const [text, setText] = useState(initial)
  const [error, setError] = useState<string>()
  const [saving, setSaving] = useState(false)
  const content = text.trim()
  const length = [...text].length
  const ready = content.length > 0 && content !== initial.trim() && !saving

  const submit = async () => {
    if (!ready) return
    setSaving(true)
    try {
      await save(content)
      onClose()
    } catch (failure) {
      setError(errorText(failure))
    } finally {
      setSaving(false)
    }
  }

  const title = t(mode === 'add' ? 'settings.memory.add-memory' : 'settings.memory.edit-memory')
  return (
    <Modal visible animationType="slide" onRequestClose={() => lowerKeyboardOr(onClose)} statusBarTranslucent navigationBarTranslucent>
      <KeyboardAvoidingView behavior="padding" style={[styles.editor, { backgroundColor: colors.bg, paddingTop: insets.top }]}>
        <View style={[styles.editorBar, { borderBottomColor: colors.line }]}>
          <Text style={[styles.editorTitle, { color: colors.text }]}>{title}</Text>
          <Pressable hitSlop={10} onPress={onClose} accessibilityLabel={t('settings.close')}>
            <X size={22} color={colors.text} />
          </Pressable>
        </View>
        <View style={styles.editorBody}>
          <TextInput
            style={[styles.editorArea, { color: colors.text, backgroundColor: colors.field }]}
            value={text}
            multiline
            autoFocus
            placeholder={t('settings.memory.add-placeholder')}
            placeholderTextColor={colors.text3}
            onChangeText={(value) => {
              setText([...value].slice(0, max).join(''))
              setError(undefined)
            }}
          />
          <View style={styles.editorMeta}>
            {error ? <Help tone="error">{error}</Help> : <View />}
            <Text style={[styles.memoryMeta, { color: length >= max ? colors.errInk : colors.text3 }]}>{t('settings.counter', { n: length, max })}</Text>
          </View>
        </View>
        {/* The inset is here, not on the KeyboardAvoidingView: in padding mode it sets paddingBottom itself. */}
        <View style={[styles.editorFoot, { paddingBottom: insets.bottom + 8 }]}>
          <View style={styles.flex}>
            <Button kind="secondary" label={t('settings.cancel')} onPress={onClose} />
          </View>
          <View style={styles.flex}>
            <Button label={t(mode === 'add' ? 'settings.memory.add' : 'settings.save')} onPress={() => void submit()} disabled={!ready} busy={saving} />
          </View>
        </View>
      </KeyboardAvoidingView>
    </Modal>
  )
}

const styles = StyleSheet.create({
  spinner: { marginTop: 32 },
  search: { flexDirection: 'row', alignItems: 'center', gap: 8, height: 42, borderRadius: 21, paddingHorizontal: 14 },
  searchInput: { flex: 1, fontSize: 15, paddingVertical: 0 },
  memory: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingHorizontal: 16, paddingVertical: 12 },
  memoryMain: { flex: 1, gap: 3 },
  memoryText: { fontSize: 15, lineHeight: 21 },
  memoryMeta: { fontSize: 12 },
  recentHead: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 16, paddingVertical: 14 },
  recentTitle: { flex: 1, fontSize: 15 },
  recent: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingHorizontal: 16, paddingVertical: 10 },
  undone: { opacity: 0.5 },
  badge: { fontSize: 11, paddingHorizontal: 6, paddingVertical: 2, borderRadius: 6, overflow: 'hidden' },
  recentText: { flex: 1, fontSize: 14, lineHeight: 20 },
  undo: { fontSize: 13, fontWeight: '500' },
  editor: { flex: 1 },
  editorBar: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingHorizontal: 20, paddingVertical: 14, borderBottomWidth: StyleSheet.hairlineWidth },
  editorTitle: { fontSize: 17, fontWeight: '600' },
  editorBody: { flex: 1, padding: 20, gap: 8 },
  editorArea: { minHeight: 140, borderRadius: 16, padding: 14, fontSize: 16, lineHeight: 23, textAlignVertical: 'top' },
  editorMeta: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12 },
  editorFoot: { flexDirection: 'row', gap: 10, paddingHorizontal: 20, paddingTop: 8 },
  flex: { flex: 1 },
})
