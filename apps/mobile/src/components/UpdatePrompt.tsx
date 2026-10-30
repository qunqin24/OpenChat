import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as Application from 'expo-application'
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'

import { Button } from '@/components/Button'
import { Markdown } from '@/components/chat/Markdown'
import { Sheet } from '@/components/Sheet'
import { useUpdateCheck, type AvailableUpdate } from '@/lib/update'
import { useColors } from '@/theme'

/**
 * Offers a newer release of the app, with its notes: download and install,
 * later (this time only), or skip that version. Mount it once, in the root
 * layout; it starts the automatic checks itself, and a manual check from the
 * About screen opens it the same way.
 */
export function UpdatePrompt() {
  const { t } = useTranslation()
  const colors = useColors()
  const { update, phase, percent, install, postpone, skip } = useUpdateCheck()
  // What the sheet shows while it slides down after the update is gone.
  const [shown, setShown] = useState<AvailableUpdate | null>(update)
  if (update && update !== shown) setShown(update)
  if (!shown) return null

  const downloading = phase === 'downloading'
  return (
    // A tap outside or the back button must not throw away a download that is
    // coming; "later" is the way to stop it.
    <Sheet open={update !== null} onClose={downloading ? () => undefined : postpone} title={t('mobile.update.title', { version: shown.version })}>
      <View style={styles.body}>
        <Text style={[styles.current, { color: colors.text3 }]}>{t('mobile.update.current', { version: Application.nativeApplicationVersion ?? '' })}</Text>
        {shown.notes ? (
          <View style={styles.notes}>
            <Text style={[styles.heading, { color: colors.text2 }]}>{t('mobile.update.notes')}</Text>
            {/* Long notes scroll on their own so the buttons stay in reach. */}
            <ScrollView style={[styles.notesScroll, { borderColor: colors.line }]} nestedScrollEnabled>
              <Markdown text={shown.notes} />
            </ScrollView>
          </View>
        ) : null}
        {phase === 'failed' ? <Text style={[styles.message, { color: colors.errInk }]}>{t('mobile.update.failed')}</Text> : null}
        {phase === 'permission' ? <Text style={[styles.message, { color: colors.text2 }]}>{t('mobile.update.permission')}</Text> : null}
        <View style={styles.actions}>
          <Button
            label={downloading ? t('mobile.update.downloading', { percent }) : t('mobile.update.install')}
            busy={downloading}
            onPress={() => void install()}
          />
          <Button label={t('mobile.update.later')} kind="secondary" onPress={postpone} />
          <Pressable accessibilityRole="button" onPress={skip} disabled={downloading} style={[styles.skip, downloading && styles.dimmed]} hitSlop={8}>
            <Text style={[styles.skipLabel, { color: colors.text3 }]}>{t('mobile.update.skip')}</Text>
          </Pressable>
        </View>
      </View>
    </Sheet>
  )
}

const styles = StyleSheet.create({
  body: { paddingHorizontal: 12, paddingBottom: 4, gap: 12 },
  current: { fontSize: 13, lineHeight: 18 },
  notes: { gap: 6 },
  heading: { fontSize: 13, fontWeight: '600' },
  notesScroll: { maxHeight: 240, borderTopWidth: StyleSheet.hairlineWidth, borderBottomWidth: StyleSheet.hairlineWidth, paddingVertical: 8 },
  message: { fontSize: 13, lineHeight: 18 },
  actions: { gap: 8, marginTop: 4 },
  skip: { alignItems: 'center', paddingVertical: 10 },
  skipLabel: { fontSize: 14 },
  dimmed: { opacity: 0.5 },
})
