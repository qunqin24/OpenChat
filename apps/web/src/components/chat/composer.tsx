import { ArrowUp, Square } from 'lucide-react'
import { useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

interface ComposerProps {
  onSend: (content: string) => void
  onStop: () => void
  isStreaming: boolean
  disabled?: boolean
}

const MAX_ROWS_PX = 200

export function Composer({ onSend, onStop, isStreaming, disabled }: ComposerProps) {
  const { t } = useTranslation()
  const [value, setValue] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // Grow with the content up to a cap, then scroll internally.
  useLayoutEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, MAX_ROWS_PX)}px`
  }, [value])

  const submit = () => {
    const content = value.trim()
    if (!content || isStreaming || disabled) return
    setValue('')
    onSend(content)
  }

  return (
    <div className="border-t border-border bg-background">
      <form
        className="mx-auto flex w-full max-w-3xl items-end gap-2 px-4 py-4"
        onSubmit={(event) => {
          event.preventDefault()
          submit()
        }}
      >
        <textarea
          ref={textareaRef}
          rows={1}
          value={value}
          disabled={disabled}
          placeholder={t('chat.placeholder')}
          aria-label={t('chat.placeholder')}
          onChange={(event) => setValue(event.target.value)}
          onKeyDown={(event) => {
            // Enter sends; Shift+Enter (and IME composition) insert a newline.
            if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
              event.preventDefault()
              submit()
            }
          }}
          className="scrollbar-thin flex-1 resize-none rounded-card border border-border bg-surface px-3 py-2.5 text-sm outline-none placeholder:text-muted-foreground disabled:opacity-50"
        />

        {isStreaming ? (
          <Button type="button" variant="secondary" size="icon" onClick={onStop} aria-label={t('chat.stop')}>
            <Square className="size-3.5 fill-current" />
          </Button>
        ) : (
          <Button
            type="submit"
            variant="primary"
            size="icon"
            disabled={!value.trim() || disabled}
            aria-label={t('chat.send')}
          >
            <ArrowUp />
          </Button>
        )}
      </form>
    </div>
  )
}
