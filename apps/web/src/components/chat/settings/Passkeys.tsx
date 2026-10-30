import { KeyRound } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useAuthOptions } from '@/hooks/use-auth-options'
import { useAddPasskey, useDeletePasskey, usePasskeys, useRenamePasskey, type Passkey } from '@/hooks/use-passkeys'
import { formatRelative } from '@openchat/chat-core/grouping'

/**
 * The account's passkeys: add one with the browser's prompt, rename one, remove
 * one. Shown only while the administrator has passkeys on (AuthOptions); the
 * passkeys themselves stay when they are turned off, unusable until they are
 * back.
 */
export function Passkeys() {
  const { t } = useTranslation()
  const { passkeys: on } = useAuthOptions()
  const list = usePasskeys(on)
  const add = useAddPasskey()
  if (!on) return null

  const addOne = async () => {
    try {
      const added = await add.mutateAsync()
      if (added) toast.success(t('settings.account.passkeys.added'))
    } catch (error) {
      toast.error(t((error as Error).message === 'passkey_exists' ? 'settings.account.passkeys.exists' : 'settings.account.passkeys.add-failed'))
    }
  }

  const items = list.data ?? []
  return (
    <div className="n-settings-passkeys">
      <div className="n-settings-line">
        <div className="n-settings-subheading">{t('settings.account.passkeys.title')}</div>
        <button type="button" className="n-settings-toggle" disabled={add.isPending} onClick={() => void addOne()}>
          {t('settings.account.passkeys.add')}
        </button>
      </div>
      <p className="n-settings-help">{t('settings.account.passkeys.desc')}</p>
      {items.length > 0 && (
        <ul className="n-settings-passkey-list">
          {items.map((passkey) => (
            <PasskeyRow key={passkey.id} passkey={passkey} />
          ))}
        </ul>
      )}
    </div>
  )
}

function PasskeyRow({ passkey }: { passkey: Passkey }) {
  const { t } = useTranslation()
  const rename = useRenamePasskey()
  const remove = useDeletePasskey()
  const [name, setName] = useState<string | null>(null)
  const [confirming, setConfirming] = useState(false)

  const save = async () => {
    const next = (name ?? '').trim()
    setName(null)
    if (!next || next === passkey.name) return
    try {
      await rename.mutateAsync({ id: passkey.id, name: next })
    } catch {
      toast.error(t('auth.error.unknown'))
    }
  }

  const meta = [
    t('settings.account.passkeys.created', { when: formatRelative(passkey.createdAt, t) }),
    passkey.lastUsedAt ? t('settings.account.passkeys.used', { when: formatRelative(passkey.lastUsedAt, t) }) : t('settings.account.passkeys.never-used'),
    passkey.synced ? t('settings.account.passkeys.synced') : '',
  ].filter(Boolean)

  return (
    <li className="n-settings-passkey">
      <KeyRound className="n-settings-passkey-icon" aria-hidden="true" />
      <div className="n-settings-passkey-text">
        {name === null ? (
          <button type="button" className="n-settings-passkey-name" title={t('settings.account.passkeys.rename')} onClick={() => setName(passkey.name)}>
            {passkey.name}
          </button>
        ) : (
          <input
            className="n-settings-bare n-settings-passkey-input"
            aria-label={t('settings.account.passkeys.rename')}
            value={name}
            maxLength={60}
            autoFocus
            onChange={(event) => setName(event.target.value)}
            onBlur={() => void save()}
            onKeyDown={(event) => {
              // Inside the profile's form: Enter renames, it does not save the profile.
              if (event.key === 'Enter') {
                event.preventDefault()
                void save()
              }
              if (event.key === 'Escape') {
                event.preventDefault()
                event.stopPropagation()
                setName(null)
              }
            }}
          />
        )}
        <div className="n-settings-help">{meta.join(' · ')}</div>
      </div>
      {confirming ? (
        <span className="n-settings-passkey-actions">
          <button type="button" className="n-settings-toggle is-danger" disabled={remove.isPending} onClick={() => remove.mutate(passkey.id, { onError: () => toast.error(t('auth.error.unknown')) })}>
            {t('settings.account.passkeys.confirm-remove')}
          </button>
          <button type="button" className="n-settings-toggle" onClick={() => setConfirming(false)}>
            {t('settings.account.passkeys.keep')}
          </button>
        </span>
      ) : (
        <button type="button" className="n-settings-toggle" onClick={() => setConfirming(true)}>
          {t('settings.account.passkeys.remove')}
        </button>
      )}
    </li>
  )
}

export default Passkeys
