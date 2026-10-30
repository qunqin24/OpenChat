import { CircleAlert, Eye, EyeOff } from 'lucide-react-native'
import { useState, type ComponentRef, type Ref } from 'react'
import { Pressable, StyleSheet, Text, TextInput, View, type TextInputProps } from 'react-native'
import { useTranslation } from 'react-i18next'

import { useColors } from '@/theme'

interface Props extends Omit<TextInputProps, 'style'> {
  label: string
  /** A quiet note at the right end of the label row, e.g. the minimum length. */
  hint?: string
  error?: string
  /** Adds the show/hide toggle and starts masked. */
  secret?: boolean
  ref?: Ref<FieldInput>
}

export type FieldInput = ComponentRef<typeof TextInput>

/** One label and input, filled rather than outlined (the web's AuthField). */
export function AuthField({ label, hint, error, secret, onFocus, onBlur, ref, ...props }: Props) {
  const { t } = useTranslation()
  const colors = useColors()
  const [revealed, setRevealed] = useState(false)
  const [focused, setFocused] = useState(false)
  const Toggle = revealed ? EyeOff : Eye

  return (
    <View style={styles.field}>
      <View style={styles.labelRow}>
        <Text style={[styles.label, { color: colors.text }]}>{label}</Text>
        {hint ? <Text style={[styles.hint, { color: colors.text3 }]}>{hint}</Text> : null}
      </View>
      <View
        style={[
          styles.control,
          { backgroundColor: focused || error ? colors.bg : colors.field },
          focused && { borderColor: colors.fieldFocus },
          error ? { borderColor: colors.errInk } : null,
        ]}
      >
        <TextInput
          ref={ref}
          style={[styles.input, { color: colors.text }]}
          placeholderTextColor={colors.text3}
          secureTextEntry={secret && !revealed}
          autoCapitalize="none"
          autoCorrect={false}
          onFocus={(event) => {
            setFocused(true)
            onFocus?.(event)
          }}
          onBlur={(event) => {
            setFocused(false)
            onBlur?.(event)
          }}
          {...props}
        />
        {secret ? (
          <Pressable
            style={styles.reveal}
            hitSlop={8}
            accessibilityRole="button"
            accessibilityLabel={revealed ? t('auth.hide-password') : t('auth.show-password')}
            onPress={() => setRevealed((value) => !value)}
          >
            <Toggle size={18} color={colors.text2} />
          </Pressable>
        ) : null}
      </View>
      {error ? (
        <View style={styles.error} accessibilityRole="alert">
          <CircleAlert size={14} color={colors.errInk} style={styles.errorIcon} />
          <Text style={[styles.errorText, { color: colors.errInk }]}>{error}</Text>
        </View>
      ) : null}
    </View>
  )
}

const styles = StyleSheet.create({
  field: { marginBottom: 16 },
  labelRow: { flexDirection: 'row', justifyContent: 'space-between', marginBottom: 8, paddingHorizontal: 4 },
  label: { fontSize: 13, lineHeight: 18, fontWeight: '500' },
  hint: { fontSize: 13, lineHeight: 18 },
  control: {
    flexDirection: 'row',
    alignItems: 'center',
    height: 48,
    borderRadius: 999,
    borderWidth: 1,
    borderColor: 'transparent',
    overflow: 'hidden',
  },
  input: { flex: 1, height: '100%', paddingHorizontal: 20, fontSize: 16 },
  reveal: { paddingHorizontal: 16, height: '100%', justifyContent: 'center' },
  error: { flexDirection: 'row', gap: 6, marginTop: 7, paddingHorizontal: 4 },
  errorIcon: { marginTop: 3 },
  errorText: { flex: 1, fontSize: 13, lineHeight: 19 },
})
