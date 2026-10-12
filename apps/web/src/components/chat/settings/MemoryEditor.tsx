import { X } from 'lucide-react'
import { Dialog } from 'radix-ui'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { useMemoryErrorText } from '@/hooks/use-memory-errors'
import { cn } from '@/lib/utils'

/**
 * Writing one memory, in a dialog of its own over the manager, as Open WebUI's
 * Add / Edit Memory modals: a box several lines tall with the count, Cancel and
 * Save. An inline field in the list showed a sentence of up to 200 characters
 * one scrolling line at a time.
 *
 * `save` does the work and throws what went wrong, which is said under the box
 * and keeps the dialog open. Enter makes a new line; Cmd/Ctrl+Enter saves.
 */
export function MemoryEditor({
  host,
  mode,
  initial = '',
  max,
  open,
  onOpenChange,
  save,
}: {
  host: HTMLElement | null
  mode: 'add' | 'edit'
  initial?: string
  max: number
  open: boolean
  onOpenChange: (open: boolean) => void
  save: (content: string) => Promise<void>
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal container={host}>
        <Dialog.Overlay className="n-modal-overlay is-above">
          <Dialog.Content className="n-modal n-memory-editor" aria-describedby={undefined}>
            {/* Mounted afresh on every opening, so the box starts from `initial`. */}
            <Editor mode={mode} initial={initial} max={max} onClose={() => onOpenChange(false)} save={save} />
          </Dialog.Content>
        </Dialog.Overlay>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

function Editor({
  mode,
  initial,
  max,
  onClose,
  save,
}: {
  mode: 'add' | 'edit'
  initial: string
  max: number
  onClose: () => void
  save: (content: string) => Promise<void>
}) {
  const { t } = useTranslation()
  const errorText = useMemoryErrorText()
  const [text, setText] = useState(initial)
  const [error, setError] = useState<string>()
  const [saving, setSaving] = useState(false)

  const content = text.trim()
  const length = [...text].length
  const changed = content !== initial.trim()
  const ready = content.length > 0 && changed && !saving

  const submit = async (event?: FormEvent) => {
    event?.preventDefault()
    if (!ready) return
    setSaving(true)
    try {
      await save(content)
      onClose()
    } catch (failure) {
      setError(errorText(failure))
    } finally {
      setSaving(false)
    }
  }

  const title = t(mode === 'add' ? 'settings.memory.add-memory' : 'settings.memory.edit-memory')
  return (
    <form className="n-memory-editor-form" onSubmit={(e) => void submit(e)}>
      <header className="n-modal-head">
        <Dialog.Title className="n-modal-title">{title}</Dialog.Title>
        <Dialog.Close className="n-settings-close" aria-label={t('settings.close')}>
          <X aria-hidden="true" />
        </Dialog.Close>
      </header>
      <div className="n-memory-editor-body">
        <textarea
          className="n-memory-editor-area"
          value={text}
          rows={5}
          autoFocus
          placeholder={t('settings.memory.add-placeholder')}
          aria-label={title}
          aria-invalid={Boolean(error)}
          onFocus={(e) => e.currentTarget.setSelectionRange(e.currentTarget.value.length, e.currentTarget.value.length)}
          onChange={(e) => {
            setText([...e.target.value].slice(0, max).join(''))
            setError(undefined)
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !e.nativeEvent.isComposing) {
              e.preventDefault()
              void submit()
            }
          }}
        />
        <div className="n-memory-editor-meta">
          {error ? (
            <p className="n-settings-error" role="alert">
              {error}
            </p>
          ) : (
            <p className="n-settings-help">{t('settings.memory.editor-hint')}</p>
          )}
          <span className={cn('n-settings-counter', length >= max && 'is-full')} aria-live="polite">
            {t('settings.counter', { n: length, max })}
          </span>
        </div>
      </div>
      <footer className="n-memory-editor-foot">
        <button type="button" className="n-settings-soft" onClick={onClose}>
          {t('settings.cancel')}
        </button>
        <button type="submit" className="n-settings-save" disabled={!ready}>
          {t(mode === 'add' ? 'settings.memory.add' : 'settings.save')}
        </button>
      </footer>
    </form>
  )
}

export default MemoryEditor
