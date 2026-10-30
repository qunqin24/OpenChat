import * as ImagePicker from 'expo-image-picker'
import { ChevronDown, ChevronUp, KeyRound } from 'lucide-react-native'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, Pressable, StyleSheet, Text, TextInput, View } from 'react-native'

import { isAuthFieldError, type AuthErrorKey, type SessionUser } from '@openchat/chat-core/auth'
import { MIN_PASSWORD_LENGTH, validateEmail, validateName } from '@openchat/chat-core/auth-validation'
import { formatRelative } from '@openchat/chat-core/grouping'

import { AuthField } from '@/components/AuthField'
import { Button } from '@/components/Button'
import { Group, Help, Row, SettingsPage } from '@/components/settings/parts'
import { Sheet } from '@/components/Sheet'
import { UserAvatar } from '@/components/UserAvatar'
import {
  AvatarFailed,
  useAddPasskey,
  useChangePassword,
  useDeletePasskey,
  usePasskeys,
  useRenamePasskey,
  useSetAvatar,
  useUpdateProfile,
  type Passkey,
} from '@/hooks/account'
import { useChatFeatures } from '@/hooks/models'
import { useAuthOptions, useUser } from '@/lib/session'
import { toast } from '@/lib/toast'
import { useColors } from '@/theme'

/**
 * The account (the web's AccountTab): the picture, name and email with one
 * Save, the password folded away, and the passkeys. The picture takes effect
 * at once, so it is not one of the things Save is for.
 */
export default function AccountScreen() {
  const user = useUser()
  if (!user) return null
  return <Profile key={user.id} user={user} />
}

function Profile({ user }: { user: SessionUser }) {
  const { t } = useTranslation()
  const update = useUpdateProfile()
  const [draft, setDraft] = useState({ name: user.name, email: user.email })
  const [errors, setErrors] = useState<{ name?: AuthErrorKey; email?: AuthErrorKey }>({})
  const dirty = draft.name.trim() !== user.name || draft.email.trim() !== user.email

  const submit = async () => {
    const found = { name: validateName(draft.name) ?? undefined, email: validateEmail(draft.email) ?? undefined }
    setErrors(found)
    if (found.name || found.email) return
    try {
      await update.mutateAsync({
        ...(draft.name.trim() !== user.name ? { name: draft.name.trim() } : {}),
        ...(draft.email.trim() !== user.email ? { email: draft.email.trim() } : {}),
      })
      toast.info(t('settings.account.saved'))
    } catch (error) {
      if (isAuthFieldError(error) && (error.field === 'name' || error.field === 'email')) setErrors({ [error.field]: error.key })
      else toast.error(t('auth.error.unknown'))
    }
  }

  return (
    <SettingsPage
      title={t('settings.tabs.account')}
      foot={dirty ? <Button label={t('settings.save')} onPress={() => void submit()} busy={update.isPending} /> : undefined}
    >
      <Avatar user={user} />
      <View style={styles.fields}>
        <AuthField
          label={t('settings.account.name')}
          value={draft.name}
          error={errors.name && t(errors.name)}
          autoComplete="username"
          onChangeText={(name) => setDraft((d) => ({ ...d, name }))}
        />
        <Help>{t('settings.account.name-hint')}</Help>
        <AuthField
          label={t('settings.account.email')}
          value={draft.email}
          error={errors.email && t(errors.email)}
          autoComplete="email"
          keyboardType="email-address"
          onChangeText={(email) => setDraft((d) => ({ ...d, email }))}
        />
      </View>
      <Password />
      <Passkeys />
    </SettingsPage>
  )
}

/** The picture, with the system's cropper to make it square; sent as soon as it is chosen. */
function Avatar({ user }: { user: SessionUser }) {
  const { t } = useTranslation()
  const colors = useColors()
  const set = useSetAvatar()
  // The administrator's switch, from the chat's features; off until it says.
  const uploads = useChatFeatures().data?.avatarUploads ?? false

  const choose = async () => {
    const picked = await ImagePicker.launchImageLibraryAsync({ mediaTypes: ['images'], allowsEditing: true, aspect: [1, 1], quality: 1 })
    if (picked.canceled || !picked.assets[0]) return
    set.mutate(picked.assets[0].uri, {
      onSuccess: () => toast.info(t('settings.account.avatar-saved')),
      onError: (error) => toast.error(t(`settings.account.avatar-${error instanceof AvatarFailed ? error.reason : 'failed'}`)),
    })
  }
  const remove = () =>
    set.mutate(null, {
      onSuccess: () => toast.info(t('settings.account.avatar-removed')),
      onError: () => toast.error(t('settings.account.avatar-failed')),
    })

  return (
    <View style={styles.avatar}>
      <UserAvatar id={user.id} name={user.name} email={user.email} src={user.avatarUrl} size={88} />
      <View style={styles.avatarActions}>
        <Pressable disabled={!uploads || set.isPending} onPress={() => void choose()} hitSlop={6}>
          <Text style={[styles.link, { color: uploads ? colors.text : colors.text3 }]}>
            {!uploads ? t('settings.account.avatar-unavailable') : t(user.avatarUrl ? 'settings.account.avatar-change' : 'settings.account.avatar-upload')}
          </Text>
        </Pressable>
        {user.avatarUrl ? (
          <Pressable disabled={set.isPending} onPress={remove} hitSlop={6}>
            <Text style={[styles.link, { color: colors.text2 }]}>{t('settings.account.avatar-remove')}</Text>
          </Pressable>
        ) : null}
      </View>
    </View>
  )
}

/** The password: folded away until asked for, with a button of its own. */
function Password() {
  const { t } = useTranslation()
  const colors = useColors()
  const change = useChangePassword()
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState({ current: '', next: '', confirm: '' })
  const [errors, setErrors] = useState<{ current?: string; next?: string; confirm?: string }>({})

  const submit = async () => {
    const found = {
      current: draft.current ? undefined : t('auth.error.password-required'),
      next: draft.next.length < MIN_PASSWORD_LENGTH ? t(draft.next ? 'auth.error.password-short' : 'auth.error.password-required') : undefined,
      confirm: draft.confirm === draft.next ? undefined : t('settings.account.password-mismatch'),
    }
    setErrors(found)
    if (found.current || found.next || found.confirm) return
    try {
      await change.mutateAsync({ currentPassword: draft.current, newPassword: draft.next })
      setDraft({ current: '', next: '', confirm: '' })
      setOpen(false)
      toast.info(t('settings.account.password-changed'))
    } catch (error) {
      if (isAuthFieldError(error) && error.field === 'currentPassword') setErrors({ current: t(error.key) })
      else if (isAuthFieldError(error) && error.field === 'newPassword') setErrors({ next: t(error.key) })
      else toast.error(t('auth.error.unknown'))
    }
  }

  return (
    <View style={styles.section}>
      <Group>
        <Row
          label={t('settings.account.password')}
          onPress={() => setOpen(!open)}
          chevron={false}
          right={open ? <ChevronUp size={17} color={colors.text3} /> : <ChevronDown size={17} color={colors.text3} />}
        />
      </Group>
      {open ? (
        <View style={styles.fields}>
          <AuthField
            label={t('settings.account.current-password')}
            placeholder={t('auth.field.password-placeholder')}
            secret
            value={draft.current}
            error={errors.current}
            autoComplete="current-password"
            onChangeText={(current) => setDraft((d) => ({ ...d, current }))}
          />
          <AuthField
            label={t('settings.account.new-password')}
            placeholder={t('settings.account.new-placeholder')}
            secret
            value={draft.next}
            error={errors.next}
            autoComplete="new-password"
            onChangeText={(next) => setDraft((d) => ({ ...d, next }))}
          />
          <AuthField
            label={t('settings.account.confirm-password')}
            placeholder={t('settings.account.confirm-placeholder')}
            secret
            value={draft.confirm}
            error={errors.confirm}
            autoComplete="new-password"
            onChangeText={(confirm) => setDraft((d) => ({ ...d, confirm }))}
          />
          <Help>{t('settings.account.password-hint')}</Help>
          <Button
            kind="secondary"
            label={t('settings.account.change-password')}
            onPress={() => void submit()}
            busy={change.isPending}
            disabled={!draft.current || !draft.next}
          />
        </View>
      ) : null}
    </View>
  )
}

/**
 * The account's passkeys: add one through Credential Manager, rename or remove
 * one. Shown only while the administrator has passkeys on.
 */
function Passkeys() {
  const { t } = useTranslation()
  const colors = useColors()
  const on = useAuthOptions().data?.passkeys ?? false
  const list = usePasskeys(on)
  const add = useAddPasskey()
  const remove = useDeletePasskey()
  const [renaming, setRenaming] = useState<{ passkey: Passkey; name: string } | null>(null)
  const rename = useRenamePasskey()
  if (!on) return null

  const addOne = async () => {
    try {
      if (await add.mutateAsync()) toast.info(t('settings.account.passkeys.added'))
    } catch (error) {
      toast.error(t((error as Error).message === 'passkey_exists' ? 'settings.account.passkeys.exists' : 'settings.account.passkeys.add-failed'))
    }
  }

  const act = (passkey: Passkey) =>
    Alert.alert(passkey.name, undefined, [
      { text: t('settings.cancel'), style: 'cancel' },
      { text: t('settings.account.passkeys.rename'), onPress: () => setRenaming({ passkey, name: passkey.name }) },
      {
        text: t('settings.account.passkeys.remove'),
        style: 'destructive',
        onPress: () => remove.mutate(passkey.id, { onError: () => toast.error(t('auth.error.unknown')) }),
      },
    ])

  const meta = (passkey: Passkey) =>
    [
      t('settings.account.passkeys.created', { when: formatRelative(passkey.createdAt, t) }),
      passkey.lastUsedAt ? t('settings.account.passkeys.used', { when: formatRelative(passkey.lastUsedAt, t) }) : t('settings.account.passkeys.never-used'),
      passkey.synced ? t('settings.account.passkeys.synced') : '',
    ]
      .filter(Boolean)
      .join(' · ')

  const saveName = () => {
    if (!renaming) return
    const next = renaming.name.trim()
    if (next && next !== renaming.passkey.name) rename.mutate({ id: renaming.passkey.id, name: next }, { onError: () => toast.error(t('auth.error.unknown')) })
    setRenaming(null)
  }

  const items = list.data ?? []
  return (
    <View style={styles.section}>
      <Group title={t('settings.account.passkeys.title')} note={t('settings.account.passkeys.desc')}>
        {items.map((passkey) => (
          <Pressable key={passkey.id} onPress={() => act(passkey)} style={({ pressed }) => [styles.passkey, pressed && { backgroundColor: colors.field }]}>
            <KeyRound size={18} color={colors.text2} />
            <View style={styles.passkeyText}>
              <Text style={[styles.passkeyName, { color: colors.text }]} numberOfLines={1}>
                {passkey.name}
              </Text>
              <Text style={[styles.passkeyMeta, { color: colors.text3 }]}>{meta(passkey)}</Text>
            </View>
          </Pressable>
        ))}
        <Row label={t('settings.account.passkeys.add')} onPress={() => void addOne()} disabled={add.isPending} chevron={false} />
      </Group>
      <Sheet open={renaming !== null} onClose={() => setRenaming(null)} title={t('settings.account.passkeys.rename')}>
        {renaming ? (
          <View style={styles.rename}>
            <TextInput
              style={[styles.renameInput, { color: colors.text, backgroundColor: colors.field }]}
              value={renaming.name}
              maxLength={60}
              onChangeText={(name) => setRenaming({ ...renaming, name })}
              autoFocus
              returnKeyType="done"
              onSubmitEditing={saveName}
            />
          </View>
        ) : null}
      </Sheet>
    </View>
  )
}

const styles = StyleSheet.create({
  avatar: { alignItems: 'center', gap: 12, paddingTop: 8 },
  avatarActions: { flexDirection: 'row', gap: 24 },
  link: { fontSize: 14, fontWeight: '500' },
  fields: { gap: 14 },
  section: { gap: 14 },
  passkey: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingHorizontal: 16, paddingVertical: 12 },
  passkeyText: { flex: 1, gap: 2 },
  passkeyName: { fontSize: 15 },
  passkeyMeta: { fontSize: 12, lineHeight: 17 },
  rename: { paddingHorizontal: 12, paddingBottom: 12 },
  renameInput: { height: 46, borderRadius: 23, paddingHorizontal: 16, fontSize: 16 },
})
