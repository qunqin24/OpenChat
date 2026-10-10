import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useMemories, usePersonalization, useUpdatePersonalization } from '@/hooks/use-memory'
import { cn } from '@/lib/utils'
import NextSwitch from '../NextSwitch'
import MemoryManager from './MemoryManager'

/**
 * Custom instructions and memory, in the classic layout: the memory heading
 * with its switch, a grey line about it, and a Manage button that opens the
 * list in a modal of its own. The instruction fields are saved with the Save
 * button at the foot — they are prose, and half a sentence is not something to
 * send to every future conversation — while the switch applies at once.
 * Everything here is the signed-in person's own; none of it reaches an
 * administrator.
 */
export function PersonalizationTab({ host }: { host: HTMLElement | null }) {
  const personalization = usePersonalization()
  if (personalization.isPending) {
    return (
      <div className="n-settings-loading" role="status">
        <span className="n-spinner" data-motion="essential" />
      </div>
    )
  }
  if (personalization.isError || !personalization.data) return <LoadFailed onRetry={() => void personalization.refetch()} />
  return <Loaded host={host} />
}

function LoadFailed({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="n-settings-none">
      <p className="n-settings-help">{t('settings.load-failed')}</p>
      <button type="button" className="n-settings-soft" onClick={onRetry}>
        {t('settings.retry')}
      </button>
    </div>
  )
}

function Loaded({ host }: { host: HTMLElement | null }) {
  const { t } = useTranslation()
  const { data } = usePersonalization()
  const update = useUpdatePersonalization()
  const memories = useMemories()
  const [draft, setDraft] = useState<{ about?: string; style?: string }>({})
  const [managing, setManaging] = useState(false)
  if (!data) return null

  const about = draft.about ?? data.aboutYou
  const style = draft.style ?? data.responseStyle
  const dirty = about !== data.aboutYou || style !== data.responseStyle

  const save = async (event: FormEvent) => {
    event.preventDefault()
    try {
      await update.mutateAsync({ aboutYou: about, responseStyle: style })
      setDraft({})
      toast.success(t('settings.personalization.saved'))
    } catch {
      toast.error(t('settings.personalization.save-failed'))
    }
  }

  const toggle = async (memoryEnabled: boolean) => {
    try {
      await update.mutateAsync({ memoryEnabled })
    } catch {
      toast.error(t('settings.personalization.save-failed'))
    }
  }

  return (
    <form className="n-settings-tab-body" id="tab-personalization" onSubmit={(e) => void save(e)}>
      <div className="n-settings-scroll">
        <div className="n-settings-subheading">{t('settings.personalization.instructions')}</div>
        <p className="n-settings-help">{t('settings.personalization.instructions-hint')}</p>
        <InstructionField
          id="instructions-about"
          label={t('settings.personalization.about')}
          placeholder={t('settings.personalization.about-placeholder')}
          value={about}
          max={data.instructionLimit}
          onChange={(value) => setDraft((d) => ({ ...d, about: value }))}
        />
        <InstructionField
          id="instructions-style"
          label={t('settings.personalization.style')}
          placeholder={t('settings.personalization.style-placeholder')}
          value={style}
          max={data.instructionLimit}
          onChange={(value) => setDraft((d) => ({ ...d, style: value }))}
        />

        <hr className="n-settings-rule" />

        <div className="n-settings-line">
          <label className="n-settings-subheading" htmlFor="memory-enabled">
            {t('settings.personalization.memory')}
          </label>
          <NextSwitch
            id="memory-enabled"
            checked={data.memoryEnabled}
            disabled={update.isPending}
            onCheckedChange={(on) => void toggle(on)}
          />
        </div>
        <p className="n-settings-help">{t('settings.personalization.memory-description')}</p>
        {!data.memoryAllowed && (
          <p className="n-settings-callout" role="status">
            {t('settings.personalization.memory-admin-off')}
          </p>
        )}
        <p className="n-settings-help is-faint">{t('settings.personalization.memory-temporary')}</p>
        <div className="n-settings-manage">
          <button type="button" className="n-settings-outline" onClick={() => setManaging(true)}>
            {t('settings.memory.manage')}
          </button>
          {memories.data && (
            <span className="n-settings-count">{t('settings.memory.count', { n: memories.data.length, max: data.memoryLimit })}</span>
          )}
        </div>

        <div className={cn('n-settings-line', 'is-disabled')}>
          <div className="n-settings-small-label">
            {t('settings.personalization.learn')}
            <span className="n-settings-badge">{t('settings.personalization.soon')}</span>
          </div>
          <NextSwitch checked={false} disabled aria-label={t('settings.personalization.learn')} />
        </div>
      </div>

      <div className="n-settings-foot">
        <button type="submit" className="n-settings-save" disabled={!dirty || update.isPending}>
          {t('settings.save')}
        </button>
      </div>

      <MemoryManager host={host} open={managing} onOpenChange={setManaging} />
    </form>
  )
}

function InstructionField({
  id, label, placeholder, value, max, onChange,
}: { id: string; label: string; placeholder: string; value: string; max: number; onChange: (value: string) => void }) {
  const { t } = useTranslation()
  const length = [...value].length
  return (
    <div className="n-settings-field">
      <label className="n-settings-small-label" htmlFor={id}>
        {label}
      </label>
      <textarea
        id={id}
        className="n-settings-bare n-settings-area"
        value={value}
        rows={2}
        placeholder={placeholder}
        onChange={(e) => onChange([...e.target.value].slice(0, max).join(''))}
      />
      <div className={cn('n-settings-counter', length >= max && 'is-full')} aria-live="polite">
        {t('settings.counter', { n: length, max })}
      </div>
    </div>
  )
}

export default PersonalizationTab
