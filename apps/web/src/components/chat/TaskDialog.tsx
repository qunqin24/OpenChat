import { isOpenChatError, type Task } from '@openchat/api-client'
import type { TFunction } from 'i18next'
import { ChevronDown, X } from 'lucide-react'
import { Dialog } from 'radix-ui'
import { useId, useState, type ComponentProps, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useChatModels } from '@/hooks/use-chat-models'
import { useCreateTask, useUpdateTask } from '@/hooks/use-tasks'
import { inPast, scheduleOf, taskKinds, weekdayName, weekdayOrder, type TaskForm } from '@/lib/tasks'
import { cn } from '@/lib/utils'

/**
 * Making or changing a scheduled task: a name, what to send, and when. A new
 * task also picks the model its conversation answers with; an existing one's
 * is changed in that conversation, like any other.
 */
export function TaskDialog({ host, open, onOpenChange, task, initial }: {
  host: HTMLElement | null
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The task being changed; a new one when absent. */
  task?: Task
  initial: TaskForm
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal container={host}>
        <Dialog.Overlay className="n-modal-overlay">
          <Dialog.Content className="n-modal n-task-dialog" aria-describedby={undefined}>
            {/* Mounted afresh on every opening, so the form starts from `initial`. */}
            <Editor task={task} initial={initial} onClose={() => onOpenChange(false)} />
          </Dialog.Content>
        </Dialog.Overlay>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

function Editor({ task, initial, onClose }: { task?: Task; initial: TaskForm; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const id = useId()
  const [form, setForm] = useState(initial)
  const [error, setError] = useState<string | null>(null)
  const models = useChatModels()
  const usable = (models.data ?? []).filter((model) => !model.atCapacity)
  const [model, setModel] = useState('')
  const create = useCreateTask()
  const update = useUpdateTask()
  const pending = create.isPending || update.isPending

  const set = <K extends keyof TaskForm>(key: K, value: TaskForm[K]) => setForm((held) => ({ ...held, [key]: value }))
  const schedule = scheduleOf(form)
  const past = inPast(form)
  const ready = form.title.trim() !== '' && form.prompt.trim() !== '' && schedule !== null && !past && !pending

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!ready || !schedule) return
    setError(null)
    const body = { title: form.title.trim(), prompt: form.prompt.trim(), schedule }
    try {
      if (task) {
        await update.mutateAsync({ id: task.id, body })
        toast.success(t('tasks.saved'))
      } else {
        await create.mutateAsync({ ...body, ...(model ? { model } : {}) })
        toast.success(t('tasks.created'))
      }
      onClose()
    } catch (failure) {
      setError(errorText(failure, t))
    }
  }

  return (
    <form className="n-task-form" onSubmit={(event) => void submit(event)}>
      <header className="n-modal-head">
        <Dialog.Title className="n-modal-title">{task ? t('tasks.edit') : t('tasks.new')}</Dialog.Title>
        <Dialog.Close className="n-settings-close" aria-label={t('settings.close')}>
          <X />
        </Dialog.Close>
      </header>

      <div className="n-task-form-body">
        <label className="n-task-label" htmlFor={`${id}-title`}>{t('tasks.field.title')}</label>
        <input
          id={`${id}-title`}
          className="n-task-input"
          value={form.title}
          maxLength={100}
          placeholder={t('tasks.field.title-placeholder')}
          onChange={(event) => set('title', event.target.value)}
          autoFocus
        />

        <label className="n-task-label" htmlFor={`${id}-prompt`}>{t('tasks.field.prompt')}</label>
        <textarea
          id={`${id}-prompt`}
          className="n-task-input n-task-area"
          value={form.prompt}
          maxLength={4000}
          rows={5}
          placeholder={t('tasks.field.prompt-placeholder')}
          onChange={(event) => set('prompt', event.target.value)}
        />
        <p className="n-settings-help">{t('tasks.field.prompt-hint')}</p>

        <span className="n-task-label" id={`${id}-kind`}>{t('tasks.field.when')}</span>
        <div className="n-task-kinds" role="radiogroup" aria-labelledby={`${id}-kind`}>
          {taskKinds.map((kind) => (
            <button
              key={kind}
              type="button"
              role="radio"
              aria-checked={form.kind === kind}
              className={cn('n-task-kind', form.kind === kind && 'active')}
              onClick={() => set('kind', kind)}
            >
              {t(`tasks.kind.${kind}`)}
            </button>
          ))}
        </div>

        {form.kind === 'weekly' && (
          <div className="n-task-days" role="group" aria-label={t('tasks.field.weekdays')}>
            {weekdayOrder.map((day) => {
              const on = form.weekdays.includes(day)
              return (
                <button
                  key={day}
                  type="button"
                  aria-pressed={on}
                  className={cn('n-task-day', on && 'active')}
                  onClick={() => set('weekdays', on ? form.weekdays.filter((d) => d !== day) : [...form.weekdays, day])}
                >
                  {weekdayName(day, i18n.resolvedLanguage)}
                </button>
              )
            })}
          </div>
        )}

        <div className="n-task-when">
          {form.kind === 'once' && (
            <input type="date" className="n-task-input" aria-label={t('tasks.field.date')} value={form.date} onChange={(event) => set('date', event.target.value)} />
          )}
          {form.kind === 'monthly' && (
            <Select aria-label={t('tasks.field.month-day')} value={form.monthDay} onChange={(event) => set('monthDay', Number(event.target.value))}>
              {Array.from({ length: 31 }, (_, i) => i + 1).map((day) => (
                <option key={day} value={day}>{t('tasks.field.day', { day })}</option>
              ))}
            </Select>
          )}
          <input type="time" className="n-task-input" aria-label={t('tasks.field.time')} value={form.time} onChange={(event) => set('time', event.target.value)} />
        </div>
        {form.kind === 'monthly' && form.monthDay > 28 && <p className="n-settings-help">{t('tasks.field.month-end')}</p>}
        {past && <p className="n-settings-error" role="alert">{t('tasks.error.past')}</p>}

        {!task && usable.length > 0 && (
          <>
            <label className="n-task-label" htmlFor={`${id}-model`}>{t('tasks.field.model')}</label>
            <Select id={`${id}-model`} value={model} onChange={(event) => setModel(event.target.value)}>
              <option value="">{t('tasks.field.model-default')}</option>
              {usable.map((option) => (
                <option key={option.id} value={option.id}>{option.name}</option>
              ))}
            </Select>
          </>
        )}

        {error && <p className="n-settings-error" role="alert">{error}</p>}
      </div>

      <footer className="n-task-form-foot">
        <button type="button" className="n-settings-soft" onClick={onClose}>{t('chat.conversation.cancel')}</button>
        <button type="submit" className="n-settings-save" disabled={!ready}>{task ? t('tasks.save') : t('tasks.create')}</button>
      </footer>
    </form>
  )
}

/**
 * A native select with its own chevron: the browser's arrow sits wherever the
 * browser likes, against the edge, whatever the padding says.
 */
function Select(props: ComponentProps<'select'>) {
  return (
    <span className="n-task-select">
      <select className="n-task-input" {...props} />
      <ChevronDown aria-hidden="true" />
    </span>
  )
}

function errorText(failure: unknown, t: TFunction<'common'>): string {
  if (isOpenChatError(failure)) {
    switch (failure.code) {
      case 'too_many_tasks':
        return t('tasks.error.too-many')
      case 'content_blocked':
        return t('tasks.error.blocked')
      case 'invalid_schedule':
        return t('tasks.error.schedule')
      case 'moderation_unavailable':
        return t('tasks.error.moderation')
    }
  }
  return t('tasks.error.generic')
}

export default TaskDialog
