import { Switch } from 'radix-ui'
import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

/**
 * The chat side's on/off switch (the admin panel has shadcn's). Radix gives it
 * the keyboard and the `switch` role; `.n-switch` in settings.css is its look.
 */
export function ToggleSwitch({ className, ...props }: ComponentProps<typeof Switch.Root>) {
  return (
    <Switch.Root className={cn('n-switch', className)} {...props}>
      <Switch.Thumb className="n-switch-thumb" />
    </Switch.Root>
  )
}

export default ToggleSwitch
