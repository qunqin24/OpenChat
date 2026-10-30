import type { ReactNode } from 'react'
import { StyleSheet, Text, View } from 'react-native'
import { KeyboardAwareScrollView } from 'react-native-keyboard-controller'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { useColors } from '@/theme'

interface Props {
  title: string
  subtitle?: string
  children: ReactNode
}

/**
 * The sign-in pages' frame, as the web's AuthShell on a phone: a two-line
 * heading (the second line quieter) over the form, scrolling clear of the
 * keyboard.
 */
export function AuthShell({ title, subtitle, children }: Props) {
  const colors = useColors()
  const insets = useSafeAreaInsets()
  return (
    <KeyboardAwareScrollView
      style={{ backgroundColor: colors.bg }}
      contentContainerStyle={[styles.content, { paddingTop: insets.top + 72, paddingBottom: insets.bottom + 32 }]}
      keyboardShouldPersistTaps="handled"
      bottomOffset={24}
    >
      <View style={styles.head}>
        <Text style={[styles.title, { color: colors.text }]}>{title}</Text>
        {subtitle ? <Text style={[styles.title, styles.subtitle, { color: colors.text3 }]}>{subtitle}</Text> : null}
      </View>
      {children}
    </KeyboardAwareScrollView>
  )
}

const styles = StyleSheet.create({
  content: { flexGrow: 1, paddingHorizontal: 24 },
  head: { marginBottom: 28 },
  title: { fontSize: 28, lineHeight: 34, fontWeight: '500' },
  subtitle: { fontWeight: '400' },
})
