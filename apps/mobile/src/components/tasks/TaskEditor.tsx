import { isOpenChatError, type Task } from '@openchat/api-client'
import DateTimePicker, { type DateTimePickerEvent } from '@react-native-community/datetimepicker'
import type { TFunction } from 'i18next'
import { X } from 'lucide-react-native'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Modal, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native'
import { KeyboardAvoidingView } from 'react-native-keyboard-controller'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { inPast, scheduleOf, taskKinds, weekdayName, weekdayOrder, type TaskForm } from '@openchat/chat-core/tasks'

import { Button } from '@/components/Button'
import { Sheet, SheetOption } from '@/components/Sheet'
import { useChatModels } from '@/hooks/models'
import { useCreateTask, useUpdateTask } from '@/hooks/tasks'
import { lowerKeyboardOr } from '@/lib/back'
import { toast } from '@/lib/toast'
import { useColors } from '@/theme'

const pad = (n: number) => String(n).padStart(2, '0')

/**
 * Making or changing a scheduled task (the web's TaskDialog): a name, what to
 * send, and when. A new task also picks the model its conversation answers
 * with; an existing one's is changed in that conversation, like any other.
 * Dates and times through the system's pickers.
 */
export function TaskEditor({ task, initial, onClose }: { task?: Task; initial: TaskForm; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const [form, setForm] = useState(initial)
  const [error, setError] = useState<string | null>(null)
  const [picking, setPicking] = useState<'date' | 'time' | 'day' | 'model' | null>(null)
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

  const submit = async () => {
    if (!ready || !schedule) return
    setError(null)
    const body = { title: form.title.trim(), prompt: form.prompt.trim(), schedule }
    try {
      if (task) {
        await update.mutateAsync({ id: task.id, body })
        toast.info(t('tasks.saved'))
      } else {
        await create.mutateAsync({ ...body, ...(model ? { model } : {}) })
        toast.info(t('tasks.created'))
      }
      onClose()
    } catch (failure) {
      setError(errorText(failure, t))
    }
  }

  // The form's date and time as a Date, for the pickers.
  const [y, mo, d] = form.date.split('-').map(Number)
  const [h, mi] = form.time.split(':').map(Number)
  const at = new Date(y ?? 2000, (mo ?? 1) - 1, d ?? 1, h ?? 0, mi ?? 0)
  const picked = (event: DateTimePickerEvent, value?: Date) => {
    const which = picking
    setPicking(null)
    if (event.type !== 'set' || !value) return
    if (which === 'date') set('date', `${value.getFullYear()}-${pad(value.getMonth() + 1)}-${pad(value.getDate())}`)
    else set('time', `${pad(value.getHours())}:${pad(value.getMinutes())}`)
  }

  const field = [styles.input, { color: colors.text, backgroundColor: colors.field }]
  return (
    <Modal visible animationType="slide" onRequestClose={() => lowerKeyboardOr(onClose)} statusBarTranslucent navigationBarTranslucent>
      <KeyboardAvoidingView behavior="padding" style={[styles.page, { backgroundColor: colors.bg, paddingTop: insets.top }]}>
        <View style={[styles.bar, { borderBottomColor: colors.line }]}>
          <Text style={[styles.heading, { color: colors.text }]}>{task ? t('tasks.edit') : t('tasks.new')}</Text>
          <Pressable hitSlop={10} onPress={onClose} accessibilityLabel={t('chat.conversation.cancel')}>
            <X size={22} color={colors.text} />
          </Pressable>
        </View>
        <ScrollView contentContainerStyle={styles.body} keyboardShouldPersistTaps="handled">
          <Text style={[styles.label, { color: colors.text }]}>{t('tasks.field.title')}</Text>
          <TextInput style={field} value={form.title} maxLength={100} placeholder={t('tasks.field.title-placeholder')} placeholderTextColor={colors.text3} onChangeText={(v) => set('title', v)} />

          <Text style={[styles.label, { color: colors.text }]}>{t('tasks.field.prompt')}</Text>
          <TextInput
            style={[field, styles.area]}
            value={form.prompt}
            maxLength={4000}
            multiline
            placeholder={t('tasks.field.prompt-placeholder')}
            placeholderTextColor={colors.text3}
            onChangeText={(v) => set('prompt', v)}
          />
          <Text style={[styles.help, { color: colors.text3 }]}>{t('tasks.field.prompt-hint')}</Text>

          <Text style={[styles.label, { color: colors.text }]}>{t('tasks.field.when')}</Text>
          <View style={[styles.segments, { backgroundColor: colors.field }]}>
            {taskKinds.map((kind) => (
              <Pressable
                key={kind}
                accessibilityRole="radio"
                accessibilityState={{ checked: form.kind === kind }}
                onPress={() => set('kind', kind)}
                style={[styles.segment, form.kind === kind && { backgroundColor: colors.card }]}
              >
                <Text style={[styles.segmentText, { color: form.kind === kind ? colors.text : colors.text2 }]}>{t(`tasks.kind.${kind}`)}</Text>
              </Pressable>
            ))}
          </View>
          {form.kind === 'weekly' ? (
            <View style={styles.days}>
              {weekdayOrder.map((day) => {
                const on = form.weekdays.includes(day)
                return (
                  <Pressable
                    key={day}
                    accessibilityRole="checkbox"
                    accessibilityState={{ checked: on }}
                    onPress={() => set('weekdays', on ? form.weekdays.filter((x) => x !== day) : [...form.weekdays, day])}
                    style={[styles.day, { backgroundColor: on ? colors.accent : colors.field }]}
                  >
                    <Text style={[styles.dayText, { color: on ? colors.accentFg : colors.text2 }]}>{weekdayName(day, i18n.language)}</Text>
                  </Pressable>
                )
              })}
            </View>
          ) : null}
          <View style={styles.when}>
            {form.kind === 'once' ? <Choice label={form.date} onPress={() => setPicking('date')} /> : null}
            {form.kind === 'monthly' ? <Choice label={t('tasks.field.day', { day: form.monthDay })} onPress={() => setPicking('day')} /> : null}
            <Choice label={form.time} onPress={() => setPicking('time')} />
          </View>
          {form.kind === 'monthly' && form.monthDay > 28 ? <Text style={[styles.help, { color: colors.text3 }]}>{t('tasks.field.month-end')}</Text> : null}
          {past ? <Text style={[styles.help, { color: colors.errInk }]}>{t('tasks.error.past')}</Text> : null}

          {!task && usable.length > 0 ? (
            <>
              <Text style={[styles.label, { color: colors.text }]}>{t('tasks.field.model')}</Text>
              <Choice label={usable.find((m) => m.id === model)?.name ?? t('tasks.field.model-default')} onPress={() => setPicking('model')} />
            </>
          ) : null}
          {error ? <Text style={[styles.help, { color: colors.errInk }]}>{error}</Text> : null}
        </ScrollView>
        {/* The inset is here, not on the KeyboardAvoidingView: in padding mode it sets paddingBottom itself. */}
        <View style={[styles.foot, { paddingBottom: insets.bottom + 8 }]}>
          <View style={styles.flex}>
            <Button kind="secondary" label={t('chat.conversation.cancel')} onPress={onClose} />
          </View>
          <View style={styles.flex}>
            <Button label={task ? t('tasks.save') : t('tasks.create')} onPress={() => void submit()} disabled={!ready} busy={pending} />
          </View>
        </View>

        {picking === 'date' || picking === 'time' ? (
          <DateTimePicker value={at} mode={picking} is24Hour onChange={picked} minimumDate={picking === 'date' ? new Date() : undefined} />
        ) : null}
        <Sheet open={picking === 'day'} onClose={() => setPicking(null)} title={t('tasks.field.month-day')}>
          {Array.from({ length: 31 }, (_, i) => i + 1).map((day) => (
            <SheetOption
              key={day}
              label={t('tasks.field.day', { day })}
              selected={day === form.monthDay}
              onPress={() => {
                set('monthDay', day)
                setPicking(null)
              }}
            />
          ))}
        </Sheet>
        <Sheet open={picking === 'model'} onClose={() => setPicking(null)} title={t('tasks.field.model')}>
          {[{ id: '', name: t('tasks.field.model-default') }, ...usable].map((option) => (
            <SheetOption
              key={option.id}
              label={option.name}
              selected={option.id === model}
              onPress={() => {
                setModel(option.id)
                setPicking(null)
              }}
            />
          ))}
        </Sheet>
      </KeyboardAvoidingView>
    </Modal>
  )
}

function Choice({ label, onPress }: { label: string; onPress: () => void }) {
  const colors = useColors()
  return (
    <Pressable onPress={onPress} accessibilityRole="button" style={[styles.input, styles.choice, { backgroundColor: colors.field }]}>
      <Text style={[styles.choiceText, { color: colors.text }]}>{label}</Text>
    </Pressable>
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

const styles = StyleSheet.create({
  page: { flex: 1 },
  flex: { flex: 1 },
  bar: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingHorizontal: 20, paddingVertical: 14, borderBottomWidth: StyleSheet.hairlineWidth },
  heading: { fontSize: 17, fontWeight: '600' },
  body: { padding: 20, gap: 8 },
  label: { fontSize: 13, fontWeight: '500', marginTop: 10 },
  input: { borderRadius: 14, paddingHorizontal: 14, minHeight: 46, fontSize: 15 },
  area: { minHeight: 120, paddingTop: 12, textAlignVertical: 'top' },
  help: { fontSize: 12, lineHeight: 17 },
  segments: { flexDirection: 'row', borderRadius: 14, padding: 3 },
  segment: { flex: 1, alignItems: 'center', paddingVertical: 9, borderRadius: 11 },
  segmentText: { fontSize: 14 },
  days: { flexDirection: 'row', flexWrap: 'wrap', gap: 6 },
  day: { paddingHorizontal: 12, paddingVertical: 8, borderRadius: 16 },
  dayText: { fontSize: 13 },
  when: { flexDirection: 'row', gap: 8 },
  choice: { flex: 1, justifyContent: 'center' },
  choiceText: { fontSize: 15 },
  foot: { flexDirection: 'row', gap: 10, paddingHorizontal: 20, paddingTop: 8 },
})
