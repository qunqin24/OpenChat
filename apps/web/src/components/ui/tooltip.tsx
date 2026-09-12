import type { ReactNode } from 'react'
import { Tooltip as RadixTooltip } from 'radix-ui'

import { cn } from '@/lib/utils'

export const TooltipProvider = RadixTooltip.Provider

interface TooltipProps {
  label: ReactNode
  side?: 'top' | 'right' | 'bottom' | 'left'
  children: ReactNode
}

export function Tooltip({ label, side = 'top', children }: TooltipProps) {
  return (
    <RadixTooltip.Root>
      <RadixTooltip.Trigger asChild>{children}</RadixTooltip.Trigger>
      <RadixTooltip.Portal>
        <RadixTooltip.Content
          side={side}
          sideOffset={6}
          className={cn(
            'z-50 rounded-md border border-border bg-surface px-2 py-1 text-xs text-foreground shadow-md',
            'data-[state=delayed-open]:animate-in data-[state=closed]:animate-out',
          )}
        >
          {label}
          <RadixTooltip.Arrow className="fill-[var(--surface)]" />
        </RadixTooltip.Content>
      </RadixTooltip.Portal>
    </RadixTooltip.Root>
  )
}
