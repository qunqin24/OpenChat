import type { SkillFile } from '@openchat/api-client'
import { FilePlus2, Trash2, X } from 'lucide-react'
import { Dialog } from 'radix-ui'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { fetchSkillVersion, skillTemplate, useSaveSkill, useSkill, useSkillVersions } from '@/hooks/use-skills'
import { useSkillErrorText } from '@/hooks/use-skill-errors'
import { cn } from '@/lib/utils'

/**
 * Writing a skill, in a dialog over the settings: its files down the left -
 * SKILL.md first, which cannot be removed - and the chosen file's text on the
 * right. The name and description are the frontmatter of SKILL.md, as the
 * Agent Skills format has them, so there are no separate fields to drift from
 * it; the server reads them and says what is wrong under the box.
 *
 * `id` null writes a new one from a template; otherwise the skill's current
 * files are loaded first. Cmd/Ctrl+Enter saves.
 */
export function SkillEditor({
  host,
  id,
  open,
  onOpenChange,
}: {
  host: HTMLElement | null
  id: string | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal container={host}>
        <Dialog.Overlay className="n-modal-overlay is-top">
          <Dialog.Content className="n-modal n-skill-editor" aria-describedby={undefined}>
            {open && <Loader id={id} onClose={() => onOpenChange(false)} />}
          </Dialog.Content>
        </Dialog.Overlay>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

function Loader({ id, onClose }: { id: string | null; onClose: () => void }) {
  const { t } = useTranslation()
  const skill = useSkill('me', id)
  if (id === null) {
    return (
      <Editor
        id={null}
        initial={skillTemplate('my-skill', t('settings.skills.template-description'), t('settings.skills.template-body'))}
        onClose={onClose}
      />
    )
  }
  if (skill.isPending) {
    return (
      <div className="n-settings-loading" role="status">
        <span className="n-spinner" data-motion="essential" />
      </div>
    )
  }
  if (skill.isError || !skill.data) {
    return (
      <div className="n-memory-none">
        {t('settings.load-failed')}
        <button type="button" className="n-settings-soft" onClick={() => void skill.refetch()}>
          {t('settings.retry')}
        </button>
      </div>
    )
  }
  return <Editor id={id} initial={skill.data.files} onClose={onClose} />
}

function Editor({ id, initial, onClose }: { id: string | null; initial: SkillFile[]; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const save = useSaveSkill('me')
  const errorText = useSkillErrorText()
  const [files, setFiles] = useState<SkillFile[]>(() => sortFiles(initial))
  const [current, setCurrent] = useState('SKILL.md')
  const [adding, setAdding] = useState<string | null>(null)
  const [error, setError] = useState<string>()

  const versions = useSkillVersions('me', id)
  // An earlier version loaded into the editor: saving it makes it the current
  // one again, as a new version.
  const loadVersion = async (versionId: string) => {
    if (!id || !versionId) return
    try {
      const version = await fetchSkillVersion('me', id, versionId)
      setFiles(sortFiles(version.files))
      setCurrent('SKILL.md')
      setError(undefined)
    } catch (failure) {
      setError(errorText(failure))
    }
  }

  const file = files.find((f) => f.path === current) ?? files[0]
  const changed = JSON.stringify(files) !== JSON.stringify(sortFiles(initial))
  const ready = (id === null || changed) && !save.isPending

  const edit = (content: string) => {
    setError(undefined)
    setFiles((list) => list.map((f) => (f.path === file?.path ? { ...f, content } : f)))
  }

  const addFile = () => {
    const path = (adding ?? '').trim().replace(/^\.?\//, '')
    if (!path) return
    if (files.some((f) => f.path === path)) {
      setCurrent(path)
    } else {
      setFiles((list) => sortFiles([...list, { path, content: '' }]))
      setCurrent(path)
    }
    setAdding(null)
  }

  const removeFile = (path: string) => {
    setFiles((list) => list.filter((f) => f.path !== path))
    if (current === path) setCurrent('SKILL.md')
  }

  const submit = async (event?: FormEvent) => {
    event?.preventDefault()
    if (!ready) return
    try {
      await save.mutateAsync({ id, files })
      onClose()
    } catch (failure) {
      setError(errorText(failure))
    }
  }

  const title = t(id === null ? 'settings.skills.new' : 'settings.skills.edit')
  return (
    <form className="n-skill-editor-form" onSubmit={(e) => void submit(e)}>
      <header className="n-modal-head">
        <Dialog.Title className="n-modal-title">{title}</Dialog.Title>
        {versions.data && versions.data.length > 1 && (
          <select
            className="n-skill-versions"
            aria-label={t('settings.skills.versions')}
            value=""
            onChange={(e) => void loadVersion(e.target.value)}
          >
            <option value="">{t('settings.skills.versions')}</option>
            {versions.data.map((v) => (
              <option key={v.id} value={v.id}>
                {new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(v.createdAt))}
                {v.current ? ` · ${t('settings.skills.current')}` : ''}
              </option>
            ))}
          </select>
        )}
        <Dialog.Close className="n-settings-close" aria-label={t('settings.close')}>
          <X aria-hidden="true" />
        </Dialog.Close>
      </header>
      <div className="n-skill-editor-body">
        <nav className="n-skill-files" aria-label={t('settings.skills.files')}>
          <ul>
            {files.map((f) => (
              <li key={f.path} className={cn('n-skill-file', f.path === file?.path && 'is-current')}>
                <button type="button" className="n-skill-file-name" onClick={() => setCurrent(f.path)}>
                  {f.path}
                </button>
                {f.path !== 'SKILL.md' && (
                  <button
                    type="button"
                    className="n-memory-icon"
                    aria-label={t('settings.skills.remove-file', { path: f.path })}
                    onClick={() => removeFile(f.path)}
                  >
                    <Trash2 aria-hidden="true" />
                  </button>
                )}
              </li>
            ))}
          </ul>
          {adding === null ? (
            <button type="button" className="n-skill-add-file" onClick={() => setAdding('')}>
              <FilePlus2 aria-hidden="true" />
              {t('settings.skills.add-file')}
            </button>
          ) : (
            <input
              className="n-skill-path"
              value={adding}
              autoFocus
              aria-label={t('settings.skills.file-path')}
              placeholder="references/guide.md"
              onChange={(e) => setAdding(e.target.value)}
              onBlur={() => (adding.trim() ? addFile() : setAdding(null))}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
                  e.preventDefault()
                  addFile()
                }
                if (e.key === 'Escape') {
                  e.stopPropagation()
                  setAdding(null)
                }
              }}
            />
          )}
        </nav>
        <div className="n-skill-text">
          <textarea
            className="n-memory-editor-area n-skill-area"
            value={file?.content ?? ''}
            spellCheck={false}
            aria-label={file?.path}
            aria-invalid={Boolean(error)}
            onChange={(e) => edit(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !e.nativeEvent.isComposing) {
                e.preventDefault()
                void submit()
              }
            }}
          />
          {error ? (
            <p className="n-settings-error is-after-field" role="alert">
              {error}
            </p>
          ) : (
            <p className="n-settings-help is-faint">{t('settings.skills.editor-hint')}</p>
          )}
        </div>
      </div>
      <footer className="n-memory-editor-foot">
        <button type="button" className="n-settings-soft" onClick={onClose}>
          {t('settings.cancel')}
        </button>
        <button type="submit" className="n-settings-save" disabled={!ready}>
          {t('settings.save')}
        </button>
      </footer>
    </form>
  )
}

/** SKILL.md first, then the rest by path. */
function sortFiles(files: SkillFile[]): SkillFile[] {
  return [...files].sort((a, b) =>
    a.path === 'SKILL.md' ? -1 : b.path === 'SKILL.md' ? 1 : a.path.localeCompare(b.path),
  )
}

export default SkillEditor
