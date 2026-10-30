import { NotebookPen, X } from 'lucide-react-native'
import { useTranslation } from 'react-i18next'
import { Pressable, StyleSheet, Text, View } from 'react-native'

import { useColors } from '@/theme'

/** A writing block's passage waiting in the composer: what the request is about. */
export function QuoteCard({ title, passage, onRemove }: { title: string; passage: string; onRemove: () => void }) {
  const { t } = useTranslation()
  const colors = useColors()
  return (
    <View style={[styles.card, { backgroundColor: colors.card, borderColor: colors.line }]}>
      <NotebookPen size={15} color={colors.text3} />
      <View style={styles.body}>
        <Text style={[styles.label, { color: colors.text3 }]} numberOfLines={1}>
          {t('document.quote-label')} · {title}
        </Text>
        <Text style={[styles.passage, { color: colors.text2 }]} numberOfLines={2}>
          {passage}
        </Text>
      </View>
      <Pressable hitSlop={8} onPress={onRemove} accessibilityLabel={t('document.quote-remove')}>
        <X size={16} color={colors.text3} />
      </Pressable>
    </View>
  )
}

const styles = StyleSheet.create({
  card: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 10, borderRadius: 12, borderWidth: StyleSheet.hairlineWidth },
  body: { flex: 1, gap: 2 },
  label: { fontSize: 12 },
  passage: { fontSize: 13, lineHeight: 18 },
})
