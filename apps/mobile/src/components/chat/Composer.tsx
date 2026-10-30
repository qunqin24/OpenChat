import type { ReasoningEffort } from '@openchat/api-client'
import * as Haptics from 'expo-haptics'
import { ArrowUp, Plus, Square } from 'lucide-react-native'
import { useEffect, useRef, useState, type ComponentRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Keyboard, Pressable, StyleSheet, Text, TextInput, View } from 'react-native'

import { useColors } from '@/theme'
import { ThinkingIcon } from './ThinkingIcon'

interface Props {
  value: string
  onChange: (value: string) => void
  onSend: () => void
  /** A run is going: the button stops it. */
  onStop?: () => void
  busy: boolean
  /** The conversation ends on a refused message: this is its editor. */
  revising: boolean
  thinkingLevels: ReasoningEffort[]
  effort: ReasoningEffort
  /** Opens the effort slider (EffortSlider), which the chat page draws over itself. */
  onOpenEffort: () => void
  /** Opens what can go with the message (AttachMenu). */
  onAttach: () => void
  /** The files and conversations going with it, above the box. */
  tray?: React.ReactNode
  /** Whether the tray holds anything: the box stays open while it does. */
  hasTray: boolean
}

/**
 * The message box and how hard the model thinks for the next run (the web's
 * Composer; the model is picked in the chat's header, ModelPicker, and web
 * search is switched in the + menu, AttachMenu). The send button stops a run
 * while one is going.
 */
export function Composer(props: Props) {
  const { t } = useTranslation()
  const colors = useColors()
  const canSend = props.value.trim().length > 0 && !props.busy
  const stopping = Boolean(props.onStop)
  const [focused, setFocused] = useState(false)
  const input = useRef<ComponentRef<typeof TextInput>>(null)
  // Android keeps the focus when back lowers the keyboard; the box is done with
  // when the keyboard is, so it lets go and folds up.
  useEffect(() => {
    const subscription = Keyboard.addListener('keyboardDidHide', () => input.current?.blur())
    return () => subscription.remove()
  }, [])
  // At rest a single line - +, the prompt, send - as chat apps on phones have
  // it; the settings for the next message come out while it is being written.
  const open = focused || props.value.trim().length > 0 || props.revising || props.hasTray

  const plus = (
    <Pressable
      onPress={props.onAttach}
      hitSlop={6}
      accessibilityRole="button"
      accessibilityLabel={t('chat.attach.menu')}
      style={[styles.round, { backgroundColor: colors.bubble }]}
    >
      <Plus size={22} color={colors.text} strokeWidth={1.75} />
    </Pressable>
  )
  const send = (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={stopping ? t('chat.stop') : t('chat.send')}
      disabled={!stopping && !canSend}
      onPress={() => {
        void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light)
        if (stopping) props.onStop?.()
        else props.onSend()
      }}
      // Quiet while there is nothing to send; ink once there is, or while it stops a run.
      style={[styles.round, { backgroundColor: stopping || canSend ? colors.accent : colors.bubble }]}
    >
      {stopping ? (
        <Square size={14} color={colors.accentFg} fill={colors.accentFg} />
      ) : (
        <ArrowUp size={20} color={canSend ? colors.accentFg : colors.text3} strokeWidth={2.25} />
      )}
    </Pressable>
  )

  return (
    // White, edged and lifted off the page, as a field to tap rather than a grey slab.
    <View style={[styles.box, !open && styles.pill, { backgroundColor: colors.card, borderColor: colors.line }]}>
      {props.revising ? <Text style={[styles.revise, { color: colors.errInk }]}>{t('chat.revise.label')}</Text> : null}
      {props.tray}
      {/* The input keeps its place in the tree in both shapes, so growing on focus does not lose the focus. */}
      <View style={open ? null : styles.line}>
        {open ? null : plus}
        <TextInput
          ref={input}
          style={[styles.input, !open && styles.inputLine, { color: colors.text }]}
          placeholder={t('chat.placeholder')}
          placeholderTextColor={colors.text2}
          value={props.value}
          onChangeText={props.onChange}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          multiline
        />
        {open ? null : send}
      </View>
      {open ? (
        <View style={styles.row}>
          {plus}
          <View style={styles.spacer} />
          {/* Beside send and the same size; the level itself is named on the slider it opens. */}
          {props.thinkingLevels.length > 0 ? (
            <Pressable
              onPress={props.onOpenEffort}
              accessibilityRole="button"
              accessibilityLabel={t('chat.thinking.current', { level: t(`chat.thinking.${props.effort}`) })}
              style={[styles.round, { backgroundColor: colors.bubble }]}
            >
              <ThinkingIcon size={22} color={colors.text} strokeWidth={1.75} />
            </Pressable>
          ) : null}
          {send}
        </View>
      ) : null}
    </View>
  )
}

const styles = StyleSheet.create({
  box: {
    borderRadius: 28,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 14,
    paddingTop: 12,
    paddingBottom: 8,
    gap: 8,
    boxShadow: '0 6px 20px rgba(0, 0, 0, 0.06)',
  },
  pill: { borderRadius: 32, paddingHorizontal: 8, paddingTop: 8, paddingBottom: 8 },
  line: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  revise: { fontSize: 12, lineHeight: 17 },
  input: { fontSize: 16, lineHeight: 22, maxHeight: 160, minHeight: 24, paddingVertical: 4, textAlignVertical: 'top' },
  inputLine: { flex: 1, maxHeight: 44 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8, marginHorizontal: -6 },
  spacer: { flex: 1 },
  round: { width: 44, height: 44, borderRadius: 22, alignItems: 'center', justifyContent: 'center' },
})
