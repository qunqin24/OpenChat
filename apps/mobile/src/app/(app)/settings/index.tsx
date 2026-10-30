import { router } from 'expo-router'
import { Brain, ChevronRight, Database, Info, Languages, LogOut, NotebookPen, SunMoon } from 'lucide-react-native'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, Pressable, StyleSheet, Text, View } from 'react-native'

import { Group, Row, SettingsPage } from '@/components/settings/parts'
import { Sheet, SheetOption } from '@/components/Sheet'
import { UserAvatar } from '@/components/UserAvatar'
import { useMemories, usePersonalization } from '@/hooks/memory'
import { languages, themes, useAppearance, type LanguageChoice, type Theme } from '@/lib/appearance'
import { signOut, useUser } from '@/lib/session'
import { useColors } from '@/theme'

const LANGUAGE_NAMES = { zh: '中文', en: 'English' }

/**
 * Settings (the web's SettingsDialog), as a list of pages: the account at the
 * top, then how the app looks, what the model knows of the person, their
 * data, and the app itself. Theme and language apply as soon as they are
 * picked, as on the web.
 */
export default function SettingsScreen() {
  const { t } = useTranslation()
  const colors = useColors()
  const user = useUser()
  const { theme, language, setTheme, setLanguage } = useAppearance()
  const personalization = usePersonalization()
  const memories = useMemories()
  const [picking, setPicking] = useState<'theme' | 'language' | null>(null)
  if (!user) return null

  const themeName = (value: Theme) => t(`chat.theme.${value}`)
  const languageName = (value: LanguageChoice) => (value === 'system' ? t('chat.theme.system') : LANGUAGE_NAMES[value])
  const icon = (Icon: typeof Brain) => <Icon size={19} color={colors.text2} />
  const memoryCount =
    memories.data && personalization.data ? t('settings.memory.count', { n: memories.data.length, max: personalization.data.memoryLimit }) : undefined

  const confirmSignOut = () =>
    Alert.alert(t('auth.logout'), undefined, [
      { text: t('settings.cancel'), style: 'cancel' },
      { text: t('auth.logout'), style: 'destructive', onPress: () => void signOut() },
    ])

  return (
    <SettingsPage title={t('settings.title')} top>
      <Group>
        <Pressable
          accessibilityRole="button"
          onPress={() => router.push('/settings/account')}
          style={({ pressed }) => [styles.profile, pressed && { backgroundColor: colors.field }]}
        >
          <UserAvatar id={user.id} name={user.name} email={user.email} src={user.avatarUrl} size={48} />
          <View style={styles.who}>
            <Text style={[styles.name, { color: colors.text }]} numberOfLines={1}>
              {user.name}
            </Text>
            <Text style={[styles.email, { color: colors.text3 }]} numberOfLines={1}>
              {user.email}
            </Text>
          </View>
          <ChevronRight size={17} color={colors.text3} />
        </Pressable>
      </Group>

      <Group title={t('settings.general.section')}>
        <Row icon={icon(SunMoon)} label={t('settings.general.theme')} value={themeName(theme)} onPress={() => setPicking('theme')} />
        <Row icon={icon(Languages)} label={t('settings.general.language')} value={languageName(language)} onPress={() => setPicking('language')} />
      </Group>

      <Group title={t('settings.tabs.personalization')}>
        <Row icon={icon(NotebookPen)} label={t('settings.personalization.instructions')} onPress={() => router.push('/settings/personalization')} />
        <Row icon={icon(Brain)} label={t('settings.memory.title')} value={memoryCount} onPress={() => router.push('/settings/memory')} />
      </Group>

      <Group>
        <Row icon={icon(Database)} label={t('settings.tabs.data')} onPress={() => router.push('/settings/data')} />
        <Row icon={icon(Info)} label={t('mobile.settings.about')} onPress={() => router.push('/settings/about')} />
      </Group>

      <Group>
        <Row icon={<LogOut size={19} color={colors.errInk} />} label={t('auth.logout')} danger onPress={confirmSignOut} chevron={false} />
      </Group>

      <Sheet open={picking === 'theme'} onClose={() => setPicking(null)} title={t('settings.general.theme')}>
        {themes.map((value) => (
          <SheetOption
            key={value}
            label={themeName(value)}
            selected={value === theme}
            onPress={() => {
              setTheme(value)
              setPicking(null)
            }}
          />
        ))}
      </Sheet>
      <Sheet open={picking === 'language'} onClose={() => setPicking(null)} title={t('settings.general.language')}>
        {languages.map((value) => (
          <SheetOption
            key={value}
            label={languageName(value)}
            selected={value === language}
            onPress={() => {
              setLanguage(value)
              setPicking(null)
            }}
          />
        ))}
      </Sheet>
    </SettingsPage>
  )
}

const styles = StyleSheet.create({
  profile: { flexDirection: 'row', alignItems: 'center', gap: 14, paddingHorizontal: 16, paddingVertical: 14 },
  who: { flex: 1, gap: 2 },
  name: { fontSize: 16, fontWeight: '500' },
  email: { fontSize: 13 },
})
