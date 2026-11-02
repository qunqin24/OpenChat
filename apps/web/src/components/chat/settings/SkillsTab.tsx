import { OpenChatError, type Skill } from '@openchat/api-client'
import { Download, Pencil, Trash2, Upload } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { skillExportUrl, useDeleteSkill, useImportSkills, useMySkills, useSetSkillEnabled } from '@/hooks/use-skills'
import { useSkillErrorText } from '@/hooks/use-skill-errors'
import ToggleSwitch from '../ToggleSwitch'
import { SkillEditor } from './SkillEditor'

/**
 * A person's skills (docs/design/skills-plan.md): instructions the model opens
 * when a task matches what one says it is for. Their own are listed with a
 * switch, edit and delete; the site's below them, read only. A change reaches
 * new conversations - one under way keeps what it started with, which the help
 * line says, since otherwise an edit that "did nothing" looks broken.
 */
export function SkillsTab({ host }: { host: HTMLElement | null }) {
  const { t } = useTranslation()
  const skills = useMySkills()
  const importer = useImportSkills('me')
  const errorText = useSkillErrorText()
  const picker = useRef<HTMLInputElement>(null)
  // The skill being edited: its id, null for a new one, undefined when closed.
  const [editing, setEditing] = useState<string | null | undefined>(undefined)

  if (skills.isPending) {
    return (
      <div className="n-settings-loading" role="status">
        <span className="n-spinner" data-motion="essential" />
      </div>
    )
  }
  if (skills.isError || !skills.data) {
    return (
      <div className="n-memory-none">
        {t('settings.load-failed')}
        <button type="button" className="n-settings-soft" onClick={() => void skills.refetch()}>
          {t('settings.retry')}
        </button>
      </div>
    )
  }
  const { items, site, builtin, allowed, enabled, limit } = skills.data
  const full = items.length >= limit

  const importFile = (file: File | undefined) => {
    if (!file) return
    importer.mutate(file, {
      onSuccess: (result) => {
        if (result.created.length > 0) toast.success(t('settings.skills.imported', { count: result.created.length }))
        for (const failed of result.failed) toast.error(t('settings.skills.import-failed-one', { name: failed.name, reason: errorText(asError(failed)) }))
        if (result.skipped.length > 0) toast.message(t('settings.skills.import-skipped', { count: result.skipped.length }))
      },
      onError: (failure) => toast.error(errorText(failure)),
    })
  }

  return (
    <div className="n-settings-tab-body" id="tab-skills">
      <div className="n-settings-scroll">
        <div className="n-settings-subheading">{t('settings.skills.title')}</div>
        <p className="n-settings-help">{t('settings.skills.description')}</p>
        {!enabled && <p className="n-settings-callout" role="status">{t('settings.skills.admin-off')}</p>}
        {enabled && !allowed && <p className="n-settings-callout" role="status">{t('settings.skills.personal-off')}</p>}
        <p className="n-settings-help is-faint">{t('settings.skills.pinned')}</p>

        <div className="n-settings-manage">
          <button type="button" className="n-settings-outline" disabled={!allowed || full} onClick={() => setEditing(null)}>
            {t('settings.skills.new')}
          </button>
          <button
            type="button"
            className="n-settings-outline n-skill-import"
            disabled={!allowed || full || importer.isPending}
            onClick={() => picker.current?.click()}
          >
            <Upload aria-hidden="true" />
            {t('settings.skills.import')}
          </button>
          <input
            ref={picker}
            type="file"
            hidden
            accept=".md,.zip,.skill"
            onChange={(e) => {
              importFile(e.target.files?.[0])
              e.target.value = ''
            }}
          />
          <span className="n-settings-count">{t('settings.skills.count', { n: items.length, max: limit })}</span>
        </div>

        {items.length === 0 ? (
          <div className="n-memory-none">{t('settings.skills.empty')}</div>
        ) : (
          <ul className="n-skill-rows">
            {items.map((skill) => (
              <SkillRow key={skill.id} skill={skill} allowed={allowed} onEdit={() => setEditing(skill.id)} />
            ))}
          </ul>
        )}

        {site.length + builtin.length > 0 && (
          <>
            <hr className="n-settings-rule" />
            <div className="n-settings-subheading">{t('settings.skills.site')}</div>
            <p className="n-settings-help">{t('settings.skills.site-description')}</p>
            <ul className="n-skill-rows">
              {site.map((skill) => (
                <li key={skill.id} className="n-skill-row">
                  <div className="n-skill-main">
                    <span className="n-skill-name">{skill.name}</span>
                    <span className="n-skill-description">{skill.description}</span>
                  </div>
                </li>
              ))}
              {builtin.map((skill) => (
                <li key={skill.name} className="n-skill-row">
                  <div className="n-skill-main">
                    <span className="n-skill-name">
                      {skill.name}
                      <span className="n-settings-badge">{t('settings.skills.builtin')}</span>
                    </span>
                    <span className="n-skill-description">{skill.description}</span>
                  </div>
                </li>
              ))}
            </ul>
          </>
        )}
      </div>

      <SkillEditor
        host={host}
        id={editing ?? null}
        open={editing !== undefined}
        onOpenChange={(open) => {
          if (!open) setEditing(undefined)
        }}
      />
    </div>
  )
}

/** An import's failure for one skill, worded like any other failed save. */
function asError(failed: { code: string; message: string }) {
  return new OpenChatError(400, { code: failed.code, message: failed.message })
}

function SkillRow({ skill, allowed, onEdit }: { skill: Skill; allowed: boolean; onEdit: () => void }) {
  const { t, i18n } = useTranslation()
  const toggle = useSetSkillEnabled('me')
  const remove = useDeleteSkill('me')
  const errorText = useSkillErrorText()
  const [confirm, setConfirm] = useState(false)
  const day = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(new Date(skill.updatedAt))

  if (confirm) {
    return (
      <li className="n-skill-row is-confirming">
        <div className="n-skill-main">
          <span className="n-skill-name">{skill.name}</span>
          <span className="n-skill-description">{t('settings.skills.delete-confirm')}</span>
        </div>
        <span className="n-memory-actions">
          <button
            type="button"
            className="n-settings-danger"
            disabled={remove.isPending}
            onClick={() => remove.mutate(skill.id, { onError: (failure) => toast.error(errorText(failure)) })}
          >
            {t('settings.skills.delete')}
          </button>
          <button type="button" className="n-settings-soft" onClick={() => setConfirm(false)}>
            {t('settings.cancel')}
          </button>
        </span>
      </li>
    )
  }

  return (
    <li className="n-skill-row">
      <button type="button" className="n-skill-main" onClick={onEdit} disabled={!allowed}>
        <span className="n-skill-name">
          {skill.name}
          {skill.blocked && <span className="n-settings-badge is-warn">{t('settings.skills.blocked')}</span>}
        </span>
        <span className="n-skill-description">{skill.description}</span>
        <span className="n-memory-date">{day}</span>
      </button>
      <span className="n-memory-actions">
        <ToggleSwitch
          checked={skill.enabled}
          disabled={toggle.isPending || (!allowed && !skill.enabled)}
          aria-label={t('settings.skills.enabled', { name: skill.name })}
          onCheckedChange={(enabled) =>
            toggle.mutate({ id: skill.id, enabled }, { onError: (failure) => toast.error(errorText(failure)) })
          }
        />
        <button type="button" className="n-memory-icon" aria-label={t('settings.skills.edit')} disabled={!allowed} onClick={onEdit}>
          <Pencil aria-hidden="true" />
        </button>
        <a className="n-memory-icon" href={skillExportUrl('me', skill.id)} download aria-label={t('settings.skills.export')}>
          <Download aria-hidden="true" />
        </a>
        <button type="button" className="n-memory-icon" aria-label={t('settings.skills.delete')} onClick={() => setConfirm(true)}>
          <Trash2 aria-hidden="true" />
        </button>
      </span>
    </li>
  )
}

export default SkillsTab
