import { ArrowDownRight, ArrowRight, ArrowUpRight, Check, ChevronDown, CircleAlert, CircleCheck, ImageIcon, Info, TriangleAlert } from 'lucide-react'
import { Collapsible, Tabs as RadixTabs } from 'radix-ui'
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Streamdown } from 'streamdown'

import { parseBlock } from '@/lib/genui/parse'
import { resolveBlock, type Cell, type UINode } from '@/lib/genui/resolve'
import { siteName } from '@/lib/urls'
import { cn } from '@/lib/utils'
import { markdownComponents, markdownLinkSafety, prepareMarkdown, useMarkdownPlugins } from '../markdown-components'
import { Chart, type ChartSeries } from './Chart'
import { ChoicesBlock } from './choices'
import { CARD_ICONS } from './icons'
import { useImageViewer } from '@/stores/image-viewer-store'
import { credit, usePicture, type ShownPicture } from './found-image'
import { MaybeLink, Picture, PictureCredit, Scroller, SourceChip } from './media'

interface GenUIBlockProps {
  source: string
  /** False while the block is still streaming. */
  complete: boolean
}

/**
 * One `openchat-ui` block, drawn with the chat's own components.
 *
 * Parsed and resolved again on every change, which is cheap at the size of a
 * block; keys come from declaration paths, so components that already exist
 * keep their DOM as the block grows.
 */
export function GenUIBlock({ source, complete }: GenUIBlockProps) {
  const { t } = useTranslation()
  const resolved = useMemo(() => resolveBlock(parseBlock(source, complete), complete), [source, complete])

  if (import.meta.env.DEV && complete && resolved.issues.length > 0) {
    console.debug('[genui]', resolved.issues)
  }

  if (!resolved.root) {
    if (!complete) return <div className="n-ui-pending" data-motion="essential" aria-label={t('chat.genui.loading')} />
    // Nothing drawable: show what the model wrote, folded away, rather than
    // losing it.
    return (
      <details className="n-ui-fallback">
        <summary>{t('chat.genui.failed')}</summary>
        <pre>{source}</pre>
      </details>
    )
  }
  return (
    <BlockComplete value={complete}>
      <div className="n-ui">{render(resolved.root)}</div>
    </BlockComplete>
  )
}

/** Whether the block is finished, for the components that act on it (Choices). */
const BlockCompleteContext = createContext(true)
const BlockComplete = BlockCompleteContext.Provider

function render(node: UINode): ReactNode {
  const Component = renderers[node.component]
  return Component ? <Component key={node.key} {...node.props} /> : null
}

function children(nodes: unknown) {
  return Array.isArray(nodes) ? (nodes as UINode[]).map(render) : null
}

function Markdown({ text }: { text: string }) {
  const plugins = useMarkdownPlugins(text)
  return (
    <div className="n-ui-text markdown-body">
      <Streamdown className="n-prose" components={markdownComponents} plugins={plugins} linkSafety={markdownLinkSafety}>
        {prepareMarkdown(text)}
      </Streamdown>
    </div>
  )
}

type Props = Record<string, unknown>
const str = (value: unknown) => (typeof value === 'string' ? value : '')

const CALLOUT_ICONS = { info: Info, success: CircleCheck, warning: TriangleAlert, danger: CircleAlert }
const TREND_ICONS = { up: ArrowUpRight, down: ArrowDownRight, flat: ArrowRight }

const renderers: Record<string, (props: Props) => ReactNode> = {
  Card: (p) => {
    const Icon = CARD_ICONS[str(p.icon)]
    const body = children(p.children)
    const badge = p.badge as UINode | undefined
    const head = str(p.title) || str(p.subtitle)
    // With content, the card is a panel: icon, titles and badge share one
    // header row above it. Without, it is a topic card: the icon stands
    // above the title.
    const panel = Boolean(body && body.length > 0)
    return (
      <section className={cn('n-ui-card', `is-${str(p.tone)}`, panel && 'is-panel', str(p.image) && 'has-image')}>
        {str(p.image) && <CardCover image={str(p.image)} alt={str(p.title)} />}
        {Icon && !panel && !str(p.image) && <Icon className="n-ui-card-icon" aria-hidden="true" />}
        {(head || badge || (Icon && panel)) && (
          <header className="n-ui-card-head">
            {Icon && panel && <Icon className="n-ui-card-icon is-inline" aria-hidden="true" />}
            <div className="n-ui-card-titles">
              {str(p.title) && <h3 className="n-ui-card-title">{str(p.title)}</h3>}
              {str(p.subtitle) && <p className="n-ui-card-subtitle">{str(p.subtitle)}</p>}
            </div>
            {badge && <div className="n-ui-card-badge">{render(badge)}</div>}
          </header>
        )}
        {panel && <div className="n-ui-stack is-md">{body}</div>}
        {str(p.footer) && <footer className="n-ui-card-footer">{str(p.footer)}</footer>}
      </section>
    )
  },
  Stack: (p) => <div className={cn('n-ui-stack', `is-${str(p.gap)}`)}>{children(p.children)}</div>,
  Row: (p) => <div className={cn('n-ui-row', `is-${str(p.gap)}`)}>{children(p.children)}</div>,
  Grid: (p) => (
    <div className="n-ui-grid" style={{ '--n-ui-cols': Number(p.columns) || 2 } as React.CSSProperties}>
      {children(p.children)}
    </div>
  ),
  Heading: (p) => {
    const level = Number(p.level) || 2
    const Tag = level === 1 ? 'h3' : level === 2 ? 'h4' : 'h5'
    return <Tag className={cn('n-ui-heading', `is-l${level}`)}>{str(p.text)}</Tag>
  },
  Text: (p) => <Markdown text={str(p.text)} />,
  Callout: (p) => {
    const tone = (str(p.tone) || 'info') as keyof typeof CALLOUT_ICONS
    const Icon = CALLOUT_ICONS[tone] ?? Info
    return (
      <div className={cn('n-ui-callout', `is-${tone}`)} role={tone === 'danger' || tone === 'warning' ? 'note' : undefined}>
        <Icon className="n-ui-callout-icon" aria-hidden="true" />
        <div className="n-ui-callout-body">
          {str(p.title) && <div className="n-ui-callout-title">{str(p.title)}</div>}
          <Markdown text={str(p.text)} />
        </div>
      </div>
    )
  },
  Stat: (p) => {
    const trend = str(p.trend) as keyof typeof TREND_ICONS
    const Icon = TREND_ICONS[trend]
    return (
      <div className="n-ui-stat">
        <div className="n-ui-stat-label">{str(p.label)}</div>
        <div className="n-ui-stat-value">{str(p.value)}</div>
        {str(p.change) && (
          <div className={cn('n-ui-stat-change', `is-${str(p.tone) || 'neutral'}`)}>
            {Icon && <Icon aria-hidden="true" />}
            {str(p.change)}
          </div>
        )}
        {str(p.hint) && <div className="n-ui-stat-hint">{str(p.hint)}</div>}
      </div>
    )
  },
  Table: (p) => {
    const columns = (p.columns as string[] | undefined) ?? []
    const rows = (p.rows as Cell[][] | undefined) ?? []
    const every = (c: number, test: (cell: Cell | undefined) => boolean) => rows.length > 0 && rows.every((row) => test(row[c]))
    // A column of numbers is right-aligned, header included, so figures line up.
    // A column of nothing but 0–100% reads as progress and gets a bar.
    const kinds = columns.map((_, c) =>
      every(c, (cell) => percent(cell) !== null) ? 'progress' : every(c, (cell) => typeof cell === 'number') ? 'number' : null,
    )
    return (
      <div className="n-ui-table-block">
        {str(p.caption) && <div className="n-ui-table-caption">{str(p.caption)}</div>}
        <div className="n-ui-table-wrap">
          <table className="n-ui-table">
            <thead>
              <tr>
                {columns.map((column, i) => (
                  <th key={i} scope="col" className={kinds[i] ? `is-${kinds[i]}` : undefined}>
                    {column}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row, r) => (
                <tr key={r}>
                  {columns.map((_, c) => (
                    <td key={c} className={kinds[c] ? `is-${kinds[c]}` : undefined}>
                      {kinds[c] === 'progress' ? <Progress value={percent(row[c]) ?? 0} label={String(row[c])} /> : (row[c] ?? '')}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    )
  },
  KeyValues: (p) => (
    <dl className="n-ui-kv">
      {((p.pairs as [string, string][] | undefined) ?? []).map(([label, value], i) => (
        <div key={i} className="n-ui-kv-row">
          <dt>{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  ),
  // Steps with pictures are a hands-on procedure: each step's picture beside
  // its title and detail, numbers left out (the titles carry them).
  Steps: (p) => (
    <ol className={cn('n-ui-steps', ((p.items as UINode[] | undefined) ?? []).some((n) => str(n.props.image)) && 'has-images')}>
      {children(p.items)}
    </ol>
  ),
  Step: (p) => (
    // No status is an ordinary procedure, drawn numbered; the done / current /
    // to-do look is for a plan whose progress is being tracked.
    <li className={cn('n-ui-step', `is-${str(p.status) || 'plain'}`)}>
      <span className="n-ui-step-marker" aria-hidden="true">
        {str(p.status) === 'done' && <CircleCheck />}
      </span>
      {str(p.image) && <StepPicture image={str(p.image)} alt={str(p.title)} />}
      <div className="n-ui-step-body">
        <div className="n-ui-step-title">{str(p.title)}</div>
        {str(p.detail) && <Markdown text={str(p.detail)} />}
      </div>
    </li>
  ),
  Timeline: (p) => <ol className="n-ui-timeline">{children(p.items)}</ol>,
  Event: (p) => (
    <li className="n-ui-event">
      <div className="n-ui-event-time">{str(p.time)}</div>
      <div className="n-ui-event-body">
        <div className="n-ui-event-title">{str(p.title)}</div>
        {str(p.detail) && <Markdown text={str(p.detail)} />}
      </div>
    </li>
  ),
  Tags: (p) => (
    <div className="n-ui-tags">
      {((p.items as string[] | undefined) ?? []).map((tag, i) => (
        <span key={i} className="n-ui-tag">
          {tag}
        </span>
      ))}
    </div>
  ),
  Chart: (p) => (
    <Chart
      kind={(str(p.kind) || 'line') as 'line'}
      labels={(p.labels as string[] | undefined) ?? []}
      series={((p.series as UINode[] | undefined) ?? []).map(
        (node): ChartSeries => ({ name: str(node.props.name), values: (node.props.values as number[] | undefined) ?? [] }),
      )}
      title={str(p.title) || undefined}
      caption={str(p.caption) || undefined}
      unit={str(p.unit) || undefined}
    />
  ),
  // A Series means nothing outside a Chart, which reads its props directly.
  Series: () => null,
  Tabs: (p) => <TabsBlock items={(p.items as UINode[] | undefined) ?? []} />,
  // Outside Tabs, a Tab is just its content.
  Tab: (p) => <div className="n-ui-stack is-md">{children(p.children)}</div>,
  Details: (p) => <DetailsBlock title={str(p.title)}>{children(p.children)}</DetailsBlock>,
  Badge: (p) => <span className={cn('n-ui-badge', `is-${str(p.tone) || 'neutral'}`)}>{str(p.text)}</span>,
  Checklist: (p) => <ChecklistBlock title={str(p.title)} items={(p.items as UINode[] | undefined) ?? []} />,
  Check: (p) => <ChecklistBlock title="" items={[{ key: 'check', component: 'Check', props: p }]} />,
  Image: (p) => (
    <ImageBlock image={str(p.image) || str(p.src)} alt={str(p.alt)} caption={str(p.caption)} aspect={str(p.aspect) || 'landscape'} link={str(p.link)} />
  ),
  Media: (p) => (
    <div className="n-ui-media">
      <StepPicture image={str(p.image)} alt={str(p.title)} />
      <div className="n-ui-media-body">
        <h3 className="n-ui-media-title">
          {/^https?:\/\//.test(str(p.link)) ? (
            <a href={str(p.link)} target="_blank" rel="noreferrer">
              {str(p.title)}
            </a>
          ) : (
            str(p.title)
          )}
        </h3>
        {str(p.detail) && <Markdown text={str(p.detail)} />}
      </div>
    </div>
  ),
  Gallery: (p) => (
    <div className="n-ui-gallery" style={{ '--n-ui-cols': Number(p.columns) || 2 } as React.CSSProperties}>
      {children(p.items)}
    </div>
  ),
  Carousel: (p) => <Scroller>{children(p.items)}</Scroller>,
  Article: (p) => (
    <ArticleBlock
      title={str(p.title)}
      summary={str(p.summary)}
      link={str(p.link)}
      image={str(p.image)}
      category={str(p.category)}
      date={str(p.date)}
    />
  ),
  Divider: () => <hr className="n-ui-divider" />,
  Choices: (p) => <Choices question={str(p.question)} items={(p.items as UINode[] | undefined) ?? []} action={str(p.action)} />,
  // Drawn only inside Choices.
  Choice: () => null,
}

function Choices(props: { question: string; items: UINode[]; action: string }) {
  return <ChoicesBlock {...props} complete={useContext(BlockCompleteContext)} />
}

/** The frame each aspect gives a picture, which it fills. */
const ASPECTS: Record<string, string> = { landscape: '4 / 3', square: '1 / 1', portrait: '3 / 4' }

/**
 * A picture with its caption (usePicture): a frame of its shape while a
 * described one is found, gone if nothing is. A click shows it full size
 * (ImageViewer), with its credit and its source page a click away.
 */
function ImageBlock({ image, alt, caption, aspect, link }: { image: string; alt: string; caption: string; aspect: string; link: string }) {
  const { t } = useTranslation()
  const { picture, settled } = usePicture(image)
  const open = useImageViewer((s) => s.open)
  const [missing, setMissing] = useState(false)
  const lose = useCallback(() => setMissing(true), [])
  if (missing || (settled && !picture)) return null
  const page = picture?.found?.landingUrl ?? (/^https?:\/\//.test(link) ? link : undefined)
  const title = caption || alt || picture?.found?.title || (link ? siteName(link) : '') || t('viewer.image')
  return (
    <figure className="n-ui-image">
      <button
        type="button"
        className="n-ui-image-frame"
        style={{ aspectRatio: ASPECTS[aspect] ?? ASPECTS.landscape }}
        aria-label={t('viewer.open', { name: title })}
        disabled={!picture}
        onClick={() =>
          picture && open([{ src: picture.src, title: [title, credit(picture.found)].filter(Boolean).join(' · '), ...(page ? { link: page } : {}) }], 0)
        }
      >
        {picture && <Picture picture={picture} alt={alt || caption} onMissing={lose} />}
        {picture?.found ? <PictureCredit found={picture.found} /> : <SourceChip link={link} />}
      </button>
      {caption && <figcaption className="n-ui-image-caption">{caption}</figcaption>}
    </figure>
  )
}

/**
 * A step's or a Media row's picture, beside its text. One that is not found
 * keeps its place as a pale tile, so the rows stay aligned with their
 * neighbours that have pictures.
 */
function StepPicture({ image, alt }: { image: string; alt: string }) {
  const { picture, settled } = usePicture(image)
  const [missing, setMissing] = useState(false)
  const lose = useCallback(() => setMissing(true), [])
  const zoom = useZoom()
  const empty = missing || (settled && !picture)
  if (empty || !picture) {
    return <div className={cn('n-ui-step-picture', empty && 'is-empty')}>{empty && <ImageIcon aria-hidden="true" />}</div>
  }
  return (
    <button type="button" className="n-ui-step-picture" aria-label={zoom.label(alt)} onClick={() => zoom.open(picture, alt)}>
      <Picture picture={picture} alt={alt} onMissing={lose} />
    </button>
  )
}

/** Opens a picture full size in the image viewer, with its credit and its source page. */
function useZoom() {
  const { t } = useTranslation()
  const open = useImageViewer((s) => s.open)
  return {
    label: (title: string) => t('viewer.open', { name: title || t('viewer.image') }),
    open: (picture: ShownPicture, title: string, link?: string) => {
      const page = picture.found?.landingUrl ?? (link && /^https?:\/\//.test(link) ? link : undefined)
      const name = title || picture.found?.title || t('viewer.image')
      open([{ src: picture.src, title: [name, credit(picture.found)].filter(Boolean).join(' · '), ...(page ? { link: page } : {}) }], 0)
    },
  }
}

/**
 * A card's cover picture. One that is not found keeps its place as a pale
 * tile, so cards side by side in a Grid or Carousel keep one height.
 */
function CardCover({ image, alt }: { image: string; alt: string }) {
  const { picture, settled } = usePicture(image)
  const [missing, setMissing] = useState(false)
  const lose = useCallback(() => setMissing(true), [])
  const zoom = useZoom()
  const empty = missing || (settled && !picture)
  if (empty || !picture) {
    return <div className={cn('n-ui-card-cover', empty && 'is-empty')}>{empty && <ImageIcon aria-hidden="true" />}</div>
  }
  return (
    <button type="button" className="n-ui-card-cover" aria-label={zoom.label(alt)} onClick={() => zoom.open(picture, alt)}>
      <Picture picture={picture} alt={alt} onMissing={lose} />
      {picture.found && <PictureCredit found={picture.found} />}
    </button>
  )
}

/** One story: lead picture with its site, category and date, headline, summary. */
function ArticleBlock({
  title,
  summary,
  link,
  image,
  category,
  date,
}: {
  title: string
  summary: string
  link: string
  image: string
  category: string
  date: string
}) {
  const { picture, settled } = usePicture(image)
  const [failed, setFailed] = useState(false)
  const lose = useCallback(() => setFailed(true), [])
  const missing = !image || failed || (settled && !picture)
  return (
    <article className="n-ui-article">
      {!missing && (
        <MaybeLink link={link} className="n-ui-article-media">
          {picture && <Picture picture={picture} alt={title} onMissing={lose} />}
          <SourceChip link={link} />
        </MaybeLink>
      )}
      {(category || date || (missing && link)) && (
        <div className="n-ui-article-meta">
          {category && <span className="n-ui-badge is-info">{category}</span>}
          {missing && link && <SourceChip link={link} />}
          {date && <span className="n-ui-article-date">{date}</span>}
        </div>
      )}
      <h3 className="n-ui-article-title">
        {/^https?:\/\//.test(link) ? (
          <a href={link} target="_blank" rel="noreferrer">
            {title}
          </a>
        ) : (
          title
        )}
      </h3>
      <Markdown text={summary} />
    </article>
  )
}

/** Switches between its tabs' content; Radix supplies the keyboard and roles. */
function TabsBlock({ items }: { items: UINode[] }) {
  const tabs = items.filter((node) => node.component === 'Tab')
  if (tabs.length === 0) return null
  return (
    <RadixTabs.Root className="n-ui-tabs" defaultValue={tabs[0]!.key}>
      <RadixTabs.List className="n-ui-tabs-list" style={{ '--n-ui-tabs': tabs.length } as React.CSSProperties}>
        {tabs.map((tab) => (
          <RadixTabs.Trigger key={tab.key} value={tab.key} className="n-ui-tabs-trigger">
            {str(tab.props.label)}
          </RadixTabs.Trigger>
        ))}
      </RadixTabs.List>
      {tabs.map((tab) => (
        <RadixTabs.Content key={tab.key} value={tab.key} className="n-ui-tabs-panel">
          <div className="n-ui-stack is-md">{children(tab.props.children)}</div>
        </RadixTabs.Content>
      ))}
    </RadixTabs.Root>
  )
}

/** Folded detail: its title is the button, the content animates open. */
function DetailsBlock({ title, children: content }: { title: string; children: ReactNode }) {
  return (
    <Collapsible.Root className="n-ui-details">
      <Collapsible.Trigger className="n-ui-details-trigger">
        <span>{title}</span>
        <ChevronDown className="n-ui-details-chevron" aria-hidden="true" />
      </Collapsible.Trigger>
      <Collapsible.Content className="n-ui-details-content">
        <div className="n-ui-details-body n-ui-stack is-md">{content}</div>
      </Collapsible.Content>
    </Collapsible.Root>
  )
}

/**
 * A to-do list with its progress. The reader can tick items off to try a
 * plan out; that stays in this page, as the model's list is the answer.
 */
function ChecklistBlock({ title, items }: { title: string; items: UINode[] }) {
  const { t } = useTranslation()
  const [ticked, setTicked] = useState<Record<string, boolean>>({})
  const checks = items.filter((node) => node.component === 'Check')
  const done = (node: UINode) => ticked[node.key] ?? node.props.done === true
  const count = checks.filter(done).length
  return (
    <div className="n-ui-checklist">
      <div className="n-ui-checklist-head">
        {title && <div className="n-ui-checklist-title">{title}</div>}
        <div className="n-ui-checklist-count">{t('chat.genui.checklist-progress', { done: count, total: checks.length })}</div>
      </div>
      <ul className="n-ui-checklist-items">
        {checks.map((node) => (
          <li key={node.key}>
            <button
              type="button"
              role="checkbox"
              aria-checked={done(node)}
              className={cn('n-ui-check', done(node) && 'is-done')}
              onClick={() => setTicked((state) => ({ ...state, [node.key]: !done(node) }))}
            >
              <span className="n-ui-check-box" aria-hidden="true">
                {done(node) && <Check />}
              </span>
              <span className="n-ui-check-text">{str(node.props.text)}</span>
            </button>
          </li>
        ))}
      </ul>
      <div className="n-ui-checklist-bar" aria-hidden="true">
        <span style={{ width: `${checks.length ? (count / checks.length) * 100 : 0}%` }} />
      </div>
    </div>
  )
}

export default GenUIBlock

/** A cell written as a percentage from 0 to 100, as a number; otherwise null. */
function percent(cell: Cell | undefined): number | null {
  if (typeof cell !== 'string') return null
  const match = /^\s*(\d{1,3}(?:\.\d+)?)\s*%\s*$/.exec(cell)
  if (!match) return null
  const value = Number(match[1])
  return value <= 100 ? value : null
}

function Progress({ value, label }: { value: number; label: string }) {
  return (
    <span className={cn('n-ui-progress', value >= 100 && 'is-complete')}>
      <span className="n-ui-progress-track" aria-hidden="true">
        <span className="n-ui-progress-fill" style={{ width: `${value}%` }} />
      </span>
      <span className="n-ui-progress-label">{label.trim()}</span>
    </span>
  )
}
