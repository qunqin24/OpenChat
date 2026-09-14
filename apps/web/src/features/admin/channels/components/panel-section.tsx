import type { ReactNode } from 'react'

import { Separator } from '@/components/shadcn/ui/separator'

type PanelSectionProps = {
  title: string
  /** Sits on the heading row, for a control that acts on the whole section. */
  action?: ReactNode
  children: ReactNode
}

/**
 * A labelled run of fields inside the channel panel. A heading, an optional
 * control beside it, and a rule — nothing more. The panel is 512px wide and a
 * card in here would spend most of that width on its own border.
 */
export function PanelSection({ title, action, children }: PanelSectionProps) {
  return (
    <section className="space-y-4">
      <div className="space-y-2">
        <div className="flex min-h-8 items-center justify-between gap-2">
          <h3 className="text-sm leading-none font-medium">{title}</h3>
          {action}
        </div>
        <Separator />
      </div>
      {children}
    </section>
  )
}
