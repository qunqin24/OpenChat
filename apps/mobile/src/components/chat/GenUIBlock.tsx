import { useTranslation } from 'react-i18next'
import { StyleSheet, Text, View } from 'react-native'

import { useColors } from '@/theme'
import { Pulse } from './Pulse'

/** An `openchat-ui` block. Placeholder until the components are drawn natively. */
export function GenUIBlock({ complete }: { source: string; complete: boolean }) {
  const { t } = useTranslation()
  const colors = useColors()
  return (
    <View style={[styles.box, { backgroundColor: colors.bubble }]}>
      {!complete ? <Pulse /> : null}
      <Text style={[styles.text, { color: colors.text2 }]}>{complete ? t('chat.genui.failed') : t('chat.genui.loading')}</Text>
    </View>
  )
}

const styles = StyleSheet.create({
  box: { flexDirection: 'row', alignItems: 'center', gap: 8, padding: 14, borderRadius: 16 },
  text: { fontSize: 14, lineHeight: 20 },
})
