import { Check, X } from 'lucide-react-native'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native'

import type { AskAnswer, AskQuestion } from '@openchat/chat-core/ask-user'

import { Button } from '@/components/Button'
import { useColors } from '@/theme'

interface Props {
  questions: AskQuestion[]
  busy: boolean
  onAnswer: (answers: AskAnswer[]) => void
  /** Puts the panel away (the line in the reply brings it back); the composer is there to write instead. */
  onDismiss: () => void
}

/**
 * The model's questions in place of the composer, one at a time (the web's
 * AskUserDock): options to pick, one or several, and a line to write beside
 * or instead. Skipping a question answers it with nothing.
 */
export function AskUserDock({ questions, busy, onAnswer, onDismiss }: Props) {
  const { t } = useTranslation()
  const colors = useColors()
  const [index, setIndex] = useState(0)
  const [answers, setAnswers] = useState<AskAnswer[]>(() => questions.map(() => ({ selected: [] })))
  const question = questions[index]!
  const answer = answers[index]!
  const lastOne = index === questions.length - 1

  const update = (next: AskAnswer) => setAnswers((all) => all.map((a, i) => (i === index ? next : a)))
  const toggle = (label: string) => {
    if (question.multiSelect) {
      update({ ...answer, selected: answer.selected.includes(label) ? answer.selected.filter((l) => l !== label) : [...answer.selected, label] })
    } else {
      update({ ...answer, selected: answer.selected[0] === label ? [] : [label] })
    }
  }
  const advance = (list = answers) => {
    if (lastOne) onAnswer(list.map((a) => ({ selected: a.selected, ...(a.other?.trim() ? { other: a.other.trim() } : {}) })))
    else setIndex(index + 1)
  }

  return (
    <View style={[styles.dock, { backgroundColor: colors.card, borderColor: colors.line }]}>
      <View style={styles.head}>
        <Text style={[styles.header, { color: colors.text3 }]}>
          {question.header || t('chat.ask.label')}
          {questions.length > 1 ? `  ${t('chat.ask.step', { n: index + 1, total: questions.length })}` : ''}
          {question.multiSelect ? `  ${t('chat.ask.multi')}` : ''}
        </Text>
        <Pressable hitSlop={10} onPress={onDismiss} accessibilityLabel={t('chat.ask.dismiss')}>
          <X size={18} color={colors.text3} />
        </Pressable>
      </View>
      <Text style={[styles.question, { color: colors.text }]}>{question.question}</Text>
      <ScrollView style={styles.options} contentContainerStyle={styles.optionsContent}>
        {question.options.map((option) => {
          const chosen = answer.selected.includes(option.label)
          return (
            <Pressable
              key={option.label}
              onPress={() => toggle(option.label)}
              accessibilityRole={question.multiSelect ? 'checkbox' : 'radio'}
              accessibilityState={{ checked: chosen }}
              style={[styles.option, { borderColor: chosen ? colors.text : colors.line, backgroundColor: chosen ? colors.field : 'transparent' }]}
            >
              <View style={styles.optionBody}>
                <Text style={[styles.optionLabel, { color: colors.text }]}>{option.label}</Text>
                {option.description ? <Text style={[styles.optionDetail, { color: colors.text3 }]}>{option.description}</Text> : null}
              </View>
              {chosen ? <Check size={18} color={colors.text} /> : null}
            </Pressable>
          )
        })}
        <TextInput
          style={[styles.other, { color: colors.text, backgroundColor: colors.field }]}
          placeholder={t('chat.ask.write')}
          placeholderTextColor={colors.text3}
          value={answer.other ?? ''}
          onChangeText={(other) => update({ ...answer, other })}
          multiline
        />
      </ScrollView>
      <View style={styles.actions}>
        {index > 0 ? (
          <View style={styles.action}>
            <Button kind="secondary" label={t('chat.ask.previous')} onPress={() => setIndex(index - 1)} />
          </View>
        ) : (
          <View style={styles.action}>
            <Button
              kind="secondary"
              label={t('chat.ask.skip')}
              onPress={() => {
                const skipped = answers.map((a, i) => (i === index ? { selected: [] } : a))
                setAnswers(skipped)
                advance(skipped)
              }}
            />
          </View>
        )}
        <View style={styles.action}>
          <Button label={lastOne ? t('chat.ask.send') : t('chat.ask.next')} busy={busy} onPress={() => advance()} />
        </View>
      </View>
    </View>
  )
}

const styles = StyleSheet.create({
  dock: { borderRadius: 24, borderWidth: StyleSheet.hairlineWidth, padding: 14, gap: 10, maxHeight: 460 },
  head: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  header: { fontSize: 12, lineHeight: 17, flexShrink: 1 },
  question: { fontSize: 16, lineHeight: 23, fontWeight: '500' },
  options: { flexGrow: 0 },
  optionsContent: { gap: 8 },
  option: { flexDirection: 'row', alignItems: 'center', gap: 10, borderWidth: 1, borderRadius: 14, padding: 12 },
  optionBody: { flex: 1, gap: 2 },
  optionLabel: { fontSize: 15, lineHeight: 21 },
  optionDetail: { fontSize: 13, lineHeight: 18 },
  other: { borderRadius: 14, padding: 12, fontSize: 15, minHeight: 44, maxHeight: 120, textAlignVertical: 'top' },
  actions: { flexDirection: 'row', gap: 10 },
  action: { flex: 1 },
})
