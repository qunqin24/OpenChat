import type { components } from '@openchat/api-client'
import { useTranslation } from 'react-i18next'
import { Alert, StyleSheet, Text, View } from 'react-native'

import { formatSize } from '@openchat/chat-core/attachments'

import { Group, Help, Row, SettingsPage } from '@/components/settings/parts'
import { useDeleteAllConversations, useExportConversations, useMyStorage } from '@/hooks/my-data'
import { toast } from '@/lib/toast'
import { useColors, type Colors } from '@/theme'

/** Past this share the meter warns that the room is running out. */
const NEARLY_FULL = 0.9

/**
 * What one's files take, then taking one's data out and deleting it (the
 * web's DataTab). Deleting asks once more before it does anything.
 */
export default function DataScreen() {
  const { t } = useTranslation()
  const exportAll = useExportConversations()
  const deleteAll = useDeleteAllConversations()

  const download = () =>
    exportAll.mutate(undefined, {
      onSuccess: () => toast.info(t('settings.data.export-done')),
      onError: () => toast.error(t('settings.data.export-failed')),
    })

  const confirmDelete = () =>
    Alert.alert(t('settings.data.delete-title'), t('settings.data.delete-confirm'), [
      { text: t('settings.cancel'), style: 'cancel' },
      {
        text: t('settings.data.delete-yes'),
        style: 'destructive',
        onPress: () =>
          deleteAll.mutate(undefined, {
            onSuccess: (result) => toast.info(t('settings.data.deleted', { count: result.deleted })),
            onError: () => toast.error(t('settings.data.delete-failed')),
          }),
      },
    ])

  return (
    <SettingsPage title={t('settings.tabs.data')}>
      <StorageMeter />
      <Group note={t('settings.data.export-desc')}>
        <Row
          label={t('settings.data.export-title')}
          value={exportAll.isPending ? t('settings.data.exporting') : undefined}
          disabled={exportAll.isPending}
          onPress={download}
        />
      </Group>
      <Group note={t('settings.data.delete-desc')}>
        <Row label={t('settings.data.delete-title')} danger chevron={false} disabled={deleteAll.isPending} onPress={confirmDelete} />
      </Group>
      <Help>{t('settings.data.temporary')}</Help>
    </SettingsPage>
  )
}

/** What the person's files take against their quota: a bar in the colours of the kinds of file. */
function StorageMeter() {
  const { t } = useTranslation()
  const colors = useColors()
  const storage = useMyStorage()
  const data = storage.data
  const share = data && data.quotaBytes > 0 ? data.usedBytes / data.quotaBytes : data && data.usedBytes > 0 ? 1 : 0
  const scale = data ? Math.max(data.quotaBytes, data.usedBytes, 1) : 1

  return (
    <Group title={t('settings.data.storage-title')}>
      <View style={styles.storage}>
        <Text style={[styles.used, { color: colors.text }]}>
          {data
            ? t('settings.data.storage-used', { used: formatSize(data.usedBytes), quota: formatSize(data.quotaBytes) })
            : storage.isError
              ? t('settings.data.storage-failed')
              : t('state.loading')}
        </Text>
        {data ? (
          <>
            <View style={[styles.bar, { backgroundColor: colors.line }]}>
              {data.kinds.map((kind) => (
                <View key={kind.kind} style={{ width: `${Math.min(100, (kind.bytes / scale) * 100)}%`, backgroundColor: kindColor(kind.kind, colors) }} />
              ))}
            </View>
            {data.kinds.length > 0 ? (
              <View style={styles.legend}>
                {data.kinds.map((kind) => (
                  <View key={kind.kind} style={styles.legendItem}>
                    <View style={[styles.dot, { backgroundColor: kindColor(kind.kind, colors) }]} />
                    <Text style={[styles.legendText, { color: colors.text2 }]}>
                      {t(`settings.data.storage-kind.${kind.kind}`)} {formatSize(kind.bytes)}
                    </Text>
                  </View>
                ))}
              </View>
            ) : null}
            {share >= 1 ? (
              <Help tone="warning">{t('settings.data.storage-full')}</Help>
            ) : share >= NEARLY_FULL ? (
              <Help tone="warning">{t('settings.data.storage-nearly-full')}</Help>
            ) : null}
          </>
        ) : null}
      </View>
    </Group>
  )
}

/** The kinds' colours, as their file badges have them. */
function kindColor(kind: components['schemas']['MyStorage']['kinds'][number]['kind'], colors: Colors): string {
  switch (kind) {
    case 'image':
      return colors.chart[0]!
    case 'pdf':
      return colors.chart[9]!
    case 'document':
      return colors.chart[1]!
    case 'text':
      return colors.chart[3]!
    default:
      return colors.chart[7]!
  }
}

const styles = StyleSheet.create({
  storage: { padding: 16, gap: 12 },
  used: { fontSize: 15 },
  bar: { flexDirection: 'row', height: 8, borderRadius: 4, overflow: 'hidden' },
  legend: { flexDirection: 'row', flexWrap: 'wrap', gap: 14 },
  legendItem: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  dot: { width: 8, height: 8, borderRadius: 4 },
  legendText: { fontSize: 12 },
})
