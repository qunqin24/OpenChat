import type { ComponentProps } from 'react'
import { ChevronRight } from 'lucide-react'
import { DropdownMenu as RadixDropdownMenu } from 'radix-ui'

import { cn } from '@/lib/utils'

export const DropdownMenuTrigger = RadixDropdownMenu.Trigger
export const DropdownMenuSub = RadixDropdownMenu.Sub

export function DropdownMenu({
  modal = false,
  ...props
}: ComponentProps<typeof RadixDropdownMenu.Root>) {
  // Like OpenWebUI's floating menu, another sidebar control works on the first
  // click; opening a menu must not make the rest of the page inert.
  return <RadixDropdownMenu.Root modal={modal} {...props} />
}

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

export function DropdownMenuSubTrigger({
  className,
  children,
  ...props
}: ComponentProps<typeof RadixDropdownMenu.SubTrigger>) {
  return (
    <RadixDropdownMenu.SubTrigger className={cn('n-menu-item n-menu-sub-trigger', className)} {...props}>
      {children}
      <ChevronRight className="n-menu-check" aria-hidden="true" />
    </RadixDropdownMenu.SubTrigger>
  )
}

export function DropdownMenuSubContent({
  className,
  sideOffset = 4,
  collisionPadding = 8,
  ...props
}: ComponentProps<typeof RadixDropdownMenu.SubContent>) {
  return (
    <RadixDropdownMenu.Portal>
      <RadixDropdownMenu.SubContent
        sideOffset={sideOffset}
        collisionPadding={collisionPadding}
        className={cn('n-menu', className)}
        {...props}
      />
    </RadixDropdownMenu.Portal>
  )
}
