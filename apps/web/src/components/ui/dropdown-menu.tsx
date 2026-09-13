import type { ComponentProps } from 'react'
import { DropdownMenu as RadixDropdownMenu } from 'radix-ui'

import { cn } from '@/lib/utils'

export const DropdownMenu = RadixDropdownMenu.Root
export const DropdownMenuTrigger = RadixDropdownMenu.Trigger

export function DropdownMenuContent({
  className,
  align = 'end',
  sideOffset = 4,
  collisionPadding = 8,
  ...props
}: ComponentProps<typeof RadixDropdownMenu.Content>) {
  return (
    <RadixDropdownMenu.Portal>
      <RadixDropdownMenu.Content
        align={align}
        sideOffset={sideOffset}
        collisionPadding={collisionPadding}
        className={cn('n-menu', className)}
        {...props}
      />
    </RadixDropdownMenu.Portal>
  )
}

export function DropdownMenuItem({
  className,
  ...props
}: ComponentProps<typeof RadixDropdownMenu.Item>) {
  return <RadixDropdownMenu.Item className={cn('n-menu-item', className)} {...props} />
}

export function DropdownMenuSeparator({
  className,
  ...props
}: ComponentProps<typeof RadixDropdownMenu.Separator>) {
  return <RadixDropdownMenu.Separator className={cn('n-menu-sep', className)} {...props} />
}

export function DropdownMenuLabel({
  className,
  ...props
}: ComponentProps<typeof RadixDropdownMenu.Label>) {
  return <RadixDropdownMenu.Label className={cn('n-menu-label', className)} {...props} />
}
