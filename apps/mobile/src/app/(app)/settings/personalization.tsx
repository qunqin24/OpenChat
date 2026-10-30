import { router } from 'expo-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ActivityIndicator, StyleSheet, Text, TextInput, View } from 'react-native'

import type { Personalization } from '@openchat/api-client'

import { Button } from '@/components/Button'
import { Group, Help, Row, SettingsPage } from '@/components/settings/parts'
import { useMemories, usePersonalization, useSaveRefusalText, useUpdatePersonalization } from '@/hooks/memory'
import { toast } from '@/lib/toast'
import { Toggle } from '@/components/Toggle'
import { useColors } from '@/theme'

/**
 * Custom instructions and the memory switch (the web's PersonalizationTab).
 * The instructions are saved with Save - they are prose, and half a sentence
 * is not something to send to every future conversation - while the switch
 * applies at once. Everything here is the person's own; none of it reaches an
 * administrator.
 */
export default function PersonalizationScreen() {
  const { t } = useTranslation()
  const colors = useColors()
  const personalization = usePersonalization()
  if (!personalization.data) {
    return (
      <SettingsPage title={t('settings.personalization.instructions')}>
        {personalization.isError ? (
          <Button kind="secondary" label={t('settings.retry')} onPress={() => void personalization.refetch()} />
        ) : (
          <ActivityIndicator color={colors.text3} style={styles.spinner} />
        )}
      </SettingsPage>
    )
  }
  return <Loaded data={personalization.data} />
}

function Loaded({ data }: { data: Personalization }) {
  const { t } = useTranslation()
  const colors = useColors()
  const update = useUpdatePersonalization()
  const memories = useMemories()
  const refusalText = useSaveRefusalText()
  const [draft, setDraft] = useState<{ about?: string; style?: string }>({})
  // Why the last save was turned away by content moderation. The fields keep what was typed.
  const [refusal, setRefusal] = useState<string>()

  const about = draft.about ?? data.aboutYou
  const style = draft.style ?? data.responseStyle
  const dirty = about !== data.aboutYou || style !== data.responseStyle

  const save = async () => {
    try {
      await update.mutateAsync({ aboutYou: about, responseStyle: style })
      setDraft({})
      setRefusal(undefined)
      toast.info(t('settings.personalization.saved'))
    } catch (failure) {
      const refused = refusalText(failure)
      if (refused) setRefusal(refused)
      else toast.error(t('settings.personalization.save-failed'))
    }
  }

  const toggle = (memoryEnabled: boolean) =>
    update.mutate({ memoryEnabled }, { onError: () => toast.error(t('settings.personalization.save-failed')) })

  const edit = (key: 'about' | 'style') => (value: string) => {
    setRefusal(undefined)
    setDraft((d) => ({ ...d, [key]: [...value].slice(0, data.instructionLimit).join('') }))
  }

  return (
    <SettingsPage
      title={t('settings.tabs.personalization')}
      foot={dirty ? <Button label={t('settings.save')} onPress={() => void save()} busy={update.isPending} /> : undefined}
    >
      <View style={styles.section}>
        <Text style={[styles.heading, { color: colors.text }]}>{t('settings.personalization.instructions')}</Text>
        <Help>{t('settings.personalization.instructions-hint')}</Help>
        <Instruction
          label={t('settings.personalization.about')}
          placeholder={t('settings.personalization.about-placeholder')}
          value={about}
          max={data.instructionLimit}
          onChange={edit('about')}
        />
        <Instruction
          label={t('settings.personalization.style')}
          placeholder={t('settings.personalization.style-placeholder')}
          value={style}
          max={data.instructionLimit}
          onChange={edit('style')}
        />
        {refusal ? <Help tone="error">{refusal}</Help> : null}
      </View>

      <View style={styles.section}>
        <Group>
          <Row
            label={t('settings.personalization.memory')}
            chevron={false}
            right={<Toggle value={data.memoryEnabled} disabled={update.isPending} onValueChange={toggle} />}
          />
          <Row
            label={t('settings.memory.manage')}
            value={memories.data ? t('settings.memory.count', { n: memories.data.length, max: data.memoryLimit }) : undefined}
            onPress={() => router.push('/settings/memory')}
          />
        </Group>
        <Help>{t('settings.personalization.memory-description')}</Help>
        {!data.memoryAllowed ? <Help tone="warning">{t('settings.personalization.memory-admin-off')}</Help> : null}
        <Help>{t('settings.personalization.memory-temporary')}</Help>
      </View>
    </SettingsPage>
  )
}

function Instruction({ label, placeholder, value, max, onChange }: { label: string; placeholder: string; value: string; max: number; onChange: (value: string) => void }) {
  const { t } = useTranslation()
  const colors = useColors()
  const length = [...value].length
  return (
    <View style={styles.field}>
      <Text style={[styles.label, { color: colors.text }]}>{label}</Text>
      <TextInput
        style={[styles.area, { color: colors.text, backgroundColor: colors.field }]}
        value={value}
        multiline
        placeholder={placeholder}
        placeholderTextColor={colors.text3}
        onChangeText={onChange}
      />
      <Text style={[styles.counter, { color: length >= max ? colors.errInk : colors.text3 }]}>{t('settings.counter', { n: length, max })}</Text>
    </View>
  )
}

const styles = StyleSheet.create({
  spinner: { marginTop: 40 },
  section: { gap: 10 },
  heading: { fontSize: 16, fontWeight: '600', paddingHorizontal: 4 },
  field: { gap: 6, marginTop: 4 },
  label: { fontSize: 14, fontWeight: '500', paddingHorizontal: 4 },
  area: { minHeight: 96, borderRadius: 16, paddingHorizontal: 14, paddingTop: 12, paddingBottom: 12, fontSize: 15, lineHeight: 21, textAlignVertical: 'top' },
  counter: { fontSize: 12, textAlign: 'right', paddingHorizontal: 4 },
})
