import { Check, Code, Copy, Workflow } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { CodeBlock, CodeBlockCopyButton, CodeBlockDownloadButton, type CustomRendererProps } from 'streamdown'

import { useDark } from '@/hooks/use-dark'

type Mermaid = (typeof import('mermaid'))['default']

let loading: Promise<Mermaid> | undefined
/** Mermaid is the heaviest thing an answer can draw: fetched for the first diagram only. */
const loadMermaid = () => (loading ??= import('mermaid').then((module) => module.default))

/**
 * One render at a time: Mermaid keeps its configuration globally, so two
 * diagrams drawn at once in different themes could take each other's.
 */
let queue: Promise<unknown> = Promise.resolve()

function draw(id: string, code: string, dark: boolean): Promise<string> {
  const next = queue.then(async () => {
    const mermaid = await loadMermaid()
    mermaid.initialize({
      startOnLoad: false,
      // No scripts, no HTML in labels, the SVG sanitised: the source is the model's.
      securityLevel: 'strict',
      suppressErrorRendering: true,
      // `neutral` is Mermaid's grey theme, the closest to the chat's own.
      theme: dark ? 'dark' : 'neutral',
      fontFamily: 'inherit',
    })
    await mermaid.parse(code)
    return (await mermaid.render(id, code)).svg
  })
  queue = next.catch(() => undefined)
  return next
}

/**
 * A mermaid code block, drawn as the diagram it describes - flowcharts,
 * sequence diagrams and the rest, as Open WebUI does. Streamdown has a Mermaid
 * plugin, but its frame is built of Tailwind utilities this build never
 * generates and shadcn tokens the chat does not have; this is the code block's
 * own sheet (HtmlPreview's), with the diagram in it.
 *
 * While the block is still being written it shows as code: half a diagram
 * does not parse. Source that does not parse at all stays code too, with a line
 * saying so. The reader can switch between the two either way.
 */
export function MermaidDiagram({ code, isIncomplete, language }: CustomRendererProps) {
  const { t } = useTranslation()
  const dark = useDark()
  const id = `mermaid-${useId().replace(/[^a-zA-Z0-9-]/g, '')}`
  const [view, setView] = useState<'diagram' | 'code'>('diagram')
  const [svg, setSvg] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    if (isIncomplete) return
    let current = true
    draw(id, code, dark).then(
      (drawn) => {
        if (!current) return
        setSvg(drawn)
        setFailed(false)
      },
      () => {
        if (current) setFailed(true)
      },
    )
    return () => {
      current = false
    }
  }, [code, dark, id, isIncomplete])

  const ready = !isIncomplete && !failed && svg !== null
  // Only once there is a diagram to switch to.
  const toggle = ready && (
    <button
      type="button"
      className="n-code-action"
      aria-pressed={view === 'diagram'}
      onClick={() => setView(view === 'diagram' ? 'code' : 'diagram')}
    >
      {view === 'code' ? <Workflow aria-hidden="true" /> : <Code aria-hidden="true" />}
      <span>{view === 'code' ? t('chat.diagram.show') : t('chat.preview.code')}</span>
    </button>
  )

  if (view === 'code' || !ready) {
    return (
      <div className="n-mermaid">
        <CodeBlock code={code} language={language} isIncomplete={isIncomplete}>
          {toggle}
          <CodeBlockDownloadButton />
          <CodeBlockCopyButton />
        </CodeBlock>
        {failed && !isIncomplete && <p className="n-mermaid-error">{t('chat.diagram.invalid')}</p>}
      </div>
    )
  }

  return (
    <div data-streamdown="code-block" className="n-mermaid is-drawn">
      <div data-streamdown="code-block-header">{language}</div>
      <div className="n-code-actions-row">
        <div data-streamdown="code-block-actions">
          {toggle}
          <CopySource code={code} />
        </div>
      </div>
      {/* Mermaid's own output, sanitised by it under the strict level. */}
      <div className="n-mermaid-canvas" role="img" aria-label={t('chat.diagram.label')} dangerouslySetInnerHTML={{ __html: svg }} />
    </div>
  )
}

function CopySource({ code }: { code: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const timer = useRef<number>(undefined)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  return (
    <button
      type="button"
      className="n-code-action is-icon"
      aria-label={t('chat.diagram.copy')}
      onClick={() => {
        void navigator.clipboard.writeText(code).then(() => {
          setCopied(true)
          window.clearTimeout(timer.current)
          timer.current = window.setTimeout(() => setCopied(false), 2000)
        })
      }}
    >
      {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
    </button>
  )
}

export default MermaidDiagram
