import type { ReactNode } from 'react'
import { Tooltip as RadixTooltip } from 'radix-ui'

/**
 * Hover help for the icon-only controls.
 *
 * The composer's buttons carry nothing but an icon, so what they do has to be
 * legible on hover. The native `title` attribute technically says it, but it
 * waits about a second, cannot be styled, and renders outside the app's own
 * look — so triggers here drop `title` and use this instead.
 *
 * A tooltip takes a `label` (what the control is) and an optional `hint` (what
 * it does), because a name alone often isn't the answer.
 */
interface TooltipProps {
  label: string
  hint?: string
  side?: 'top' | 'right' | 'bottom' | 'left'
  children: ReactNode
}

/**
 * Shares open/close timing across a row of controls. Inside a group the first
 * tooltip waits, but moving along the row swaps instantly — scanning a toolbar
 * shouldn't cost a delay per button.
 */
export function TooltipGroup({ children }: { children: ReactNode }) {
  return (
    <RadixTooltip.Provider delayDuration={400} skipDelayDuration={400}>
      {children}
    </RadixTooltip.Provider>
  )
}

export function Tooltip({ label, hint, side = 'top', children }: TooltipProps) {
  return (
    <RadixTooltip.Root>
      <RadixTooltip.Trigger asChild>{children}</RadixTooltip.Trigger>
      <RadixTooltip.Portal>
        <RadixTooltip.Content className="n-tip" side={side} sideOffset={8} collisionPadding={10}>
          <span className="n-tip-label">{label}</span>
          {hint && <span className="n-tip-hint">{hint}</span>}
          <RadixTooltip.Arrow className="n-tip-arrow" width={11} height={5} />
        </RadixTooltip.Content>
      </RadixTooltip.Portal>
    </RadixTooltip.Root>
  )
}

export default Tooltip
