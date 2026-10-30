import { isOpenChatError, type Task } from '@openchat/api-client'
import type { DrawerNavigationProp } from '@react-navigation/drawer'
import type { ParamListBase } from '@react-navigation/native'
import { router, useNavigation } from 'expo-router'
import { AlarmClock, Menu, MoreHorizontal, Plus } from 'lucide-react-native'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ActivityIndicator, Alert, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { describeSchedule, emptyForm, formatWhen, formOf, taskIdeas, type TaskForm } from '@openchat/chat-core/tasks'

import { Button } from '@/components/Button'
import { HeaderIcon, PageHeader } from '@/components/HeaderPill'
import { Sheet, SheetOption } from '@/components/Sheet'
import { TaskEditor } from '@/components/tasks/TaskEditor'
import { useChatFeatures } from '@/hooks/models'
import { useDeleteTask, useRunTask, useTasks, useUpdateTask } from '@/hooks/tasks'
import { toast } from '@/lib/toast'
import { useColors } from '@/theme'

/**
 * Scheduled tasks (the web's TasksPage): what will run, what is paused or done,
 * and a way to make one - from scratch or from an idea. Each runs in a
 * conversation of its own, a tap away. Models make them too (the
 * scheduled_tasks tool), and those show here the same way.
 */
export default function TasksScreen() {
  const { t } = useTranslation()
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const navigation = useNavigation<DrawerNavigationProp<ParamListBase>>()
  const features = useChatFeatures()
  const off = features.data?.scheduledTasks === false
  const tasks = useTasks(!off)
  const remove = useDeleteTask()
  const [editing, setEditing] = useState<{ task?: Task; form: TaskForm } | null>(null)
  const [acting, setActing] = useState<Task | null>(null)
  const items = tasks.data?.items ?? []
  const active = items.filter((task) => task.status === 'active')
  const rest = items.filter((task) => task.status !== 'active')
  const full = active.length >= (tasks.data?.limit ?? Infinity)
  const openNew = (over: Partial<TaskForm> = {}) => setEditing({ form: emptyForm(new Date(), over) })

  const confirmDelete = (task: Task) =>
    Alert.alert(t('tasks.delete-title'), t('tasks.delete-description', { title: task.title }), [
      { text: t('chat.conversation.cancel'), style: 'cancel' },
      {
        text: t('chat.delete'),
        style: 'destructive',
        onPress: () =>
          remove.mutate(task.id, {
            onSuccess: () => toast.info(t('tasks.deleted', { title: task.title })),
            onError: (error) => {
              if (!(isOpenChatError(error) && error.status === 404)) toast.error(t('tasks.error.generic'))
              void tasks.refetch()
            },
          }),
      },
    ])

  return (
    <View style={[styles.page, { backgroundColor: colors.bg, paddingTop: insets.top }]}>
      <PageHeader
        title={t('tasks.title')}
        left={
          <HeaderIcon onPress={() => navigation.openDrawer()} label={t('chat.sidebar.open')}>
            <Menu size={20} color={colors.text} />
          </HeaderIcon>
        }
        right={
          !off && items.length > 0 ? (
            <HeaderIcon disabled={full} onPress={() => openNew()} label={t('tasks.new')}>
              <Plus size={20} color={colors.text} />
            </HeaderIcon>
          ) : undefined
        }
      />
      <ScrollView contentContainerStyle={[styles.body, { paddingBottom: insets.bottom + 24 }]}>
        {off ? (
          <Text style={[styles.note, { color: colors.text2 }]}>{t('tasks.off')}</Text>
        ) : tasks.isPending ? (
          <ActivityIndicator color={colors.text3} style={styles.spinner} />
        ) : tasks.isError ? (
          <Pressable onPress={() => void tasks.refetch()}>
            <Text style={[styles.note, { color: colors.text2 }]}>{t('tasks.retry')}</Text>
          </Pressable>
        ) : items.length === 0 ? (
          <View style={styles.empty}>
            <View style={[styles.emptyIcon, { backgroundColor: colors.bubble }]}>
              <AlarmClock size={26} color={colors.text2} />
            </View>
            <Text style={[styles.emptyTitle, { color: colors.text }]}>{t('tasks.empty')}</Text>
            <Text style={[styles.note, { color: colors.text2 }]}>{t('tasks.empty-hint')}</Text>
            {taskIdeas.map((idea) => (
              <Pressable
                key={idea.key}
                onPress={() => openNew({ ...idea.form, title: t(`tasks.ideas.${idea.key}.title`), prompt: t(`tasks.ideas.${idea.key}.prompt`) })}
                style={[styles.idea, { backgroundColor: colors.bubble }]}
              >
                <Text style={[styles.rowTitle, { color: colors.text }]}>{t(`tasks.ideas.${idea.key}.title`)}</Text>
                <Text style={[styles.rowSub, { color: colors.text3 }]}>{t(`tasks.ideas.${idea.key}.hint`)}</Text>
              </Pressable>
            ))}
            <Button label={t('tasks.new')} onPress={() => openNew()} />
            <Text style={[styles.note, { color: colors.text3 }]}>{t('tasks.ask-hint')}</Text>
          </View>
        ) : (
          <>
            {active.length === 0 ? <Text style={[styles.note, { color: colors.text2 }]}>{t('tasks.none-active')}</Text> : null}
            {active.map((task) => (
              <TaskRow key={task.id} task={task} onMore={() => setActing(task)} />
            ))}
            {rest.length > 0 ? <Text style={[styles.section, { color: colors.text3 }]}>{t('tasks.section.rest')}</Text> : null}
            {rest.map((task) => (
              <TaskRow key={task.id} task={task} onMore={() => setActing(task)} />
            ))}
            <Text style={[styles.foot, { color: colors.text3 }]}>{t('tasks.limit', { count: active.length, limit: tasks.data?.limit ?? 0 })}</Text>
          </>
        )}
      </ScrollView>

      {acting ? (
        <TaskActions
          task={acting}
          onClose={() => setActing(null)}
          onEdit={() => setEditing({ task: acting, form: formOf(acting) })}
          onDelete={() => confirmDelete(acting)}
        />
      ) : null}
      {editing ? <TaskEditor task={editing.task} initial={editing.form} onClose={() => setEditing(null)} /> : null}
    </View>
  )
}

/** A task's conversation, in the chat screen. */
const openChat = (task: Task) => router.navigate({ pathname: '/', params: { id: task.conversationId } })

function TaskRow({ task, onMore }: { task: Task; onMore: () => void }) {
  const { t, i18n } = useTranslation()
  const colors = useColors()
  const status =
    task.status === 'active' && task.nextRunAt
      ? t('tasks.next', { when: formatWhen(new Date(task.nextRunAt), i18n.language) })
      : task.status === 'paused'
        ? t('tasks.status.paused')
        : t('tasks.status.completed')
  return (
    <View style={[styles.row, task.status !== 'active' && styles.idle]}>
      <Pressable style={styles.rowMain} onPress={() => openChat(task)}>
        <AlarmClock size={18} color={colors.text2} />
        <View style={styles.rowText}>
          <Text style={[styles.rowTitle, { color: colors.text }]} numberOfLines={1}>
            {task.title}
          </Text>
          <Text style={[styles.rowSub, { color: colors.text3 }]}>
            {describeSchedule(task.schedule, t, i18n.language)} · {status}
          </Text>
          {task.lastError ? (
            <Text style={[styles.rowSub, { color: colors.errInk }]}>
              {t('tasks.last-error', { reason: t(`tasks.reason.${task.lastError}` as 'tasks.reason.failed', { defaultValue: t('tasks.reason.failed') }) })}
            </Text>
          ) : null}
        </View>
      </Pressable>
      <Pressable hitSlop={10} onPress={onMore} accessibilityLabel={t('tasks.actions', { title: task.title })}>
        <MoreHorizontal size={20} color={colors.text2} />
      </Pressable>
    </View>
  )
}

function TaskActions({ task, onClose, onEdit, onDelete }: { task: Task; onClose: () => void; onEdit: () => void; onDelete: () => void }) {
  const { t } = useTranslation()
  const update = useUpdateTask()
  const run = useRunTask()
  const paused = task.status === 'paused'
  const then = (action: () => void) => () => {
    onClose()
    action()
  }
  return (
    <Sheet open onClose={onClose} title={task.title}>
      <SheetOption label={t('tasks.edit')} onPress={then(onEdit)} />
      <SheetOption
        label={t('tasks.run-now')}
        onPress={then(() =>
          run.mutate(task.id, {
            onSuccess: () => toast.info(t('tasks.started', { title: task.title })),
            onError: (error) => toast.error(isOpenChatError(error) && error.code === 'conversation_busy' ? t('tasks.error.busy') : t('tasks.error.generic')),
          }),
        )}
      />
      {task.status !== 'completed' ? (
        <SheetOption
          label={paused ? t('tasks.resume') : t('tasks.pause')}
          onPress={then(() =>
            update.mutate(
              { id: task.id, body: { enabled: paused } },
              {
                onSuccess: (next) => toast.info(t(next.status === 'paused' ? 'tasks.paused' : 'tasks.resumed', { title: task.title })),
                onError: (error) => toast.error(isOpenChatError(error) && error.code === 'too_many_tasks' ? t('tasks.error.too-many') : t('tasks.error.generic')),
              },
            ),
          )}
        />
      ) : null}
      <SheetOption label={t('tasks.open-chat')} onPress={then(() => openChat(task))} />
      <SheetOption label={t('chat.delete')} onPress={then(onDelete)} />
    </Sheet>
  )
}

const styles = StyleSheet.create({
  page: { flex: 1 },
  body: { paddingHorizontal: 16, paddingTop: 8, gap: 6 },
  spinner: { marginTop: 40 },
  note: { fontSize: 14, lineHeight: 21, textAlign: 'center' },
  empty: { alignItems: 'stretch', gap: 12, paddingTop: 24 },
  emptyIcon: { alignSelf: 'center', width: 64, height: 64, borderRadius: 32, alignItems: 'center', justifyContent: 'center', marginBottom: 4 },
  emptyTitle: { fontSize: 18, fontWeight: '600', textAlign: 'center' },
  idea: { padding: 14, borderRadius: 16, gap: 2 },
  section: { fontSize: 12, marginTop: 18, marginBottom: 4, paddingHorizontal: 4 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: 12, paddingHorizontal: 4 },
  idle: { opacity: 0.6 },
  rowMain: { flex: 1, flexDirection: 'row', alignItems: 'center', gap: 12 },
  rowText: { flex: 1, gap: 2 },
  rowTitle: { fontSize: 15, fontWeight: '500' },
  rowSub: { fontSize: 12, lineHeight: 17 },
  foot: { fontSize: 12, textAlign: 'center', marginTop: 16 },
})
