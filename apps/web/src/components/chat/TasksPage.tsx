import { isOpenChatError, type Task } from '@openchat/api-client'
import { Link, useNavigate } from '@tanstack/react-router'
import { AlarmClock, MessageSquare, MoreHorizontal, Pause, PencilLine, Play, Plus, RotateCw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { useChatFeatures } from '@/hooks/use-chat-models'
import { useDeleteTask, useRunTask, useTasks, useUpdateTask } from '@/hooks/use-tasks'
import { describeSchedule, emptyForm, formatWhen, formOf, taskIdeas, type TaskForm } from '@/lib/tasks'
import { cn } from '@/lib/utils'
import { SidebarToggle } from './ChatShell'
import TaskDialog from './TaskDialog'

/**
 * Scheduled tasks: what will run, what is paused or done, and a way to make
 * one - from scratch or from an idea. Each runs in a conversation of its own,
 * a click away. Models can make them too (the scheduled_tasks tool), and those
 * show up here the same way.
 */
export default function TasksPage() {
  const { t, i18n } = useTranslation()
  const features = useChatFeatures()
  const off = features.data?.scheduledTasks === false
  const tasks = useTasks(!off)
  const remove = useDeleteTask()
  const [host, setHost] = useState<HTMLElement | null>(null)
  const [editing, setEditing] = useState<{ task?: Task; form: TaskForm } | null>(null)
  const [pending, setPending] = useState<Task | null>(null)

  const items = tasks.data?.items ?? []
  const active = items.filter((task) => task.status === 'active')
  const rest = items.filter((task) => task.status !== 'active')
  const full = active.length >= (tasks.data?.limit ?? Infinity)

  const openNew = (over: Partial<TaskForm> = {}) => setEditing({ form: emptyForm(new Date(), over) })

  const confirmDelete = async () => {
    if (!pending) return
    const task = pending
    setPending(null)
    try {
      await remove.mutateAsync(task.id)
      toast.success(t('tasks.deleted', { title: task.title }))
    } catch (error) {
      if (!(isOpenChatError(error) && error.status === 404)) toast.error(t('tasks.error.generic'))
      void tasks.refetch()
    }
  }

  return (
    <main className="n-main n-tasks" ref={setHost}>
      <div className="n-topbar">
        <SidebarToggle />
      </div>
      <div className="n-scroll">
        <div className="n-tasks-column">
          <header className="n-tasks-head">
            <h1 className="n-library-title">{t('tasks.title')}</h1>
            {!off && items.length > 0 && (
              <button type="button" className="n-tasks-new" disabled={full} title={full ? t('tasks.error.too-many') : undefined} onClick={() => openNew()}>
                <Plus aria-hidden="true" />
                {t('tasks.new')}
              </button>
            )}
          </header>

          {off ? (
            <div className="n-library-state"><p className="n-library-empty-title">{t('tasks.off')}</p></div>
          ) : tasks.isPending ? (
            <div className="n-library-state" role="status" aria-label={t('state.loading')}>
              <span className="n-spinner" data-motion="essential" />
            </div>
          ) : tasks.isError ? (
            <div className="n-library-state">
              <button type="button" className="n-library-retry" onClick={() => void tasks.refetch()}>{t('tasks.retry')}</button>
            </div>
          ) : items.length === 0 ? (
            <div className="n-tasks-empty">
              <AlarmClock className="n-tasks-empty-icon" aria-hidden="true" />
              <p className="n-tasks-empty-title">{t('tasks.empty')}</p>
              <p className="n-tasks-empty-hint">{t('tasks.empty-hint')}</p>
              <ul className="n-tasks-ideas">
                {taskIdeas.map((idea) => (
                  <li key={idea.key}>
                    <button
                      type="button"
                      className="n-tasks-idea"
                      onClick={() => openNew({ ...idea.form, title: t(`tasks.ideas.${idea.key}.title`), prompt: t(`tasks.ideas.${idea.key}.prompt`) })}
                    >
                      <span className="n-tasks-idea-emoji" aria-hidden="true">{idea.emoji}</span>
                      <span className="n-tasks-idea-text">
                        <span className="n-tasks-idea-title">{t(`tasks.ideas.${idea.key}.title`)}</span>
                        <span className="n-tasks-idea-hint">{t(`tasks.ideas.${idea.key}.hint`)}</span>
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
              <button type="button" className="n-tasks-new" onClick={() => openNew()}>
                <Plus aria-hidden="true" />
                {t('tasks.new')}
              </button>
              <p className="n-tasks-empty-hint">{t('tasks.ask-hint')}</p>
            </div>
          ) : (
            <>
              <section aria-label={t('tasks.section.active')}>
                {active.length === 0 ? (
                  <p className="n-tasks-none">{t('tasks.none-active')}</p>
                ) : (
                  <ul className="n-tasks-list">
                    {active.map((task) => (
                      <TaskRow key={task.id} task={task} locale={i18n.resolvedLanguage} onEdit={() => setEditing({ task, form: formOf(task) })} onDelete={() => setPending(task)} />
                    ))}
                  </ul>
                )}
              </section>
              {rest.length > 0 && (
                <section className="n-tasks-section" aria-label={t('tasks.section.rest')}>
                  <h2 className="n-group">{t('tasks.section.rest')}</h2>
                  <ul className="n-tasks-list">
                    {rest.map((task) => (
                      <TaskRow key={task.id} task={task} locale={i18n.resolvedLanguage} onEdit={() => setEditing({ task, form: formOf(task) })} onDelete={() => setPending(task)} />
                    ))}
                  </ul>
                </section>
              )}
              <p className="n-tasks-foot">{t('tasks.limit', { count: active.length, limit: tasks.data?.limit ?? 0 })}</p>
            </>
          )}
        </div>
      </div>

      <TaskDialog
        host={host}
        open={editing !== null}
        onOpenChange={(open) => { if (!open) setEditing(null) }}
        task={editing?.task}
        initial={editing?.form ?? emptyForm()}
      />

      <AlertDialog open={pending !== null} onOpenChange={(open) => !open && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('tasks.delete-title')}</AlertDialogTitle>
            <AlertDialogDescription>{t('tasks.delete-description', { title: pending?.title ?? '' })}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('chat.conversation.cancel')}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void confirmDelete()}>{t('chat.delete')}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </main>
  )
}

function TaskRow({ task, locale, onEdit, onDelete }: { task: Task; locale?: string; onEdit: () => void; onDelete: () => void }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const update = useUpdateTask()
  const run = useRunTask()
  const paused = task.status === 'paused'

  const toggle = () =>
    update.mutate(
      { id: task.id, body: { enabled: paused } },
      {
        onSuccess: (next) => toast.success(t(next.status === 'paused' ? 'tasks.paused' : 'tasks.resumed', { title: task.title })),
        onError: (error) => toast.error(isOpenChatError(error) && error.code === 'too_many_tasks' ? t('tasks.error.too-many') : t('tasks.error.generic')),
      },
    )
  const runNow = () =>
    run.mutate(task.id, {
      onSuccess: () => toast.success(t('tasks.started', { title: task.title })),
      onError: (error) => toast.error(isOpenChatError(error) && error.code === 'conversation_busy' ? t('tasks.error.busy') : t('tasks.error.generic')),
    })

  let status: string
  if (task.status === 'active' && task.nextRunAt) status = t('tasks.next', { when: formatWhen(new Date(task.nextRunAt), locale) })
  else if (task.status === 'paused') status = t('tasks.status.paused')
  else status = t('tasks.status.completed')

  return (
    <li className={cn('n-tasks-row', task.status !== 'active' && 'is-idle')}>
      <Link className="n-tasks-row-main" to="/chat/$conversationId" params={{ conversationId: task.conversationId }}>
        <span className="n-tasks-row-icon" aria-hidden="true"><AlarmClock /></span>
        <span className="n-tasks-row-text">
          <span className="n-tasks-row-title" dir="auto">{task.title}</span>
          <span className="n-tasks-row-sub">
            {describeSchedule(task.schedule, t, locale)} · {status}
          </span>
          {task.lastError && (
            <span className="n-tasks-row-error">{t('tasks.last-error', { reason: t(`tasks.reason.${task.lastError}`, { defaultValue: t('tasks.reason.failed') }) })}</span>
          )}
        </span>
      </Link>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button type="button" className="n-icon-btn n-library-menu" aria-label={t('tasks.actions', { title: task.title })}>
            <MoreHorizontal className="h-4 w-4" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={onEdit}><PencilLine /><span>{t('tasks.edit')}</span></DropdownMenuItem>
          <DropdownMenuItem disabled={run.isPending} onSelect={runNow}><RotateCw /><span>{t('tasks.run-now')}</span></DropdownMenuItem>
          {task.status !== 'completed' && (
            <DropdownMenuItem disabled={update.isPending} onSelect={toggle}>
              {paused ? <Play /> : <Pause />}<span>{paused ? t('tasks.resume') : t('tasks.pause')}</span>
            </DropdownMenuItem>
          )}
          <DropdownMenuItem onSelect={() => void navigate({ to: '/chat/$conversationId', params: { conversationId: task.conversationId } })}>
            <MessageSquare /><span>{t('tasks.open-chat')}</span>
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={onDelete}><Trash2 /><span>{t('chat.delete')}</span></DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </li>
  )
}
