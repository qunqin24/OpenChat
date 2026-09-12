import type { ComponentProps } from 'react'
import { AlertDialog as RadixAlertDialog } from 'radix-ui'

import { cn } from '@/lib/utils'

export const AlertDialog = RadixAlertDialog.Root
export const AlertDialogTrigger = RadixAlertDialog.Trigger

export function AlertDialogContent({
  className,
  children,
  ...props
}: ComponentProps<typeof RadixAlertDialog.Content>) {
  return (
    <RadixAlertDialog.Portal>
      <RadixAlertDialog.Overlay
        className="fixed inset-0 z-50 backdrop-blur-[2px]"
        style={{ background: 'rgba(0, 0, 0, 0.4)' }}
      />
      <RadixAlertDialog.Content
        className={cn(
          'fixed top-1/2 left-1/2 z-50 w-[min(26rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2',
          'rounded-2xl p-5',
          className,
        )}
        style={{ background: 'var(--n-card)', boxShadow: 'var(--n-shadow-card-hover)' }}
        {...props}
      >
        {children}
      </RadixAlertDialog.Content>
    </RadixAlertDialog.Portal>
  )
}

export function AlertDialogHeader({ className, ...props }: ComponentProps<'div'>) {
  return <div className={cn('flex flex-col gap-1.5', className)} {...props} />
}

export function AlertDialogFooter({ className, ...props }: ComponentProps<'div'>) {
  return <div className={cn('mt-5 flex justify-end gap-2', className)} {...props} />
}

export function AlertDialogTitle({
  className,
  ...props
}: ComponentProps<typeof RadixAlertDialog.Title>) {
  return (
    <RadixAlertDialog.Title
      className={cn('text-[15px] font-semibold', className)}
      {...props}
    />
  )
}

export function AlertDialogDescription({
  className,
  ...props
}: ComponentProps<typeof RadixAlertDialog.Description>) {
  return (
    <RadixAlertDialog.Description
      className={cn('text-sm leading-relaxed', className)}
      style={{ color: 'var(--n-text-2)' }}
      {...props}
    />
  )
}

export function AlertDialogCancel({
  className,
  ...props
}: ComponentProps<typeof RadixAlertDialog.Cancel>) {
  return (
    <RadixAlertDialog.Cancel
      className={cn('h-9 rounded-lg px-3.5 text-sm font-medium transition-colors', className)}
      style={{ background: 'var(--n-hover)', color: 'var(--n-text)' }}
      {...props}
    />
  )
}

export function AlertDialogAction({
  className,
  ...props
}: ComponentProps<typeof RadixAlertDialog.Action>) {
  return (
    <RadixAlertDialog.Action
      className={cn('h-9 rounded-lg px-3.5 text-sm font-medium transition-colors', className)}
      style={{ background: 'var(--n-accent)', color: 'var(--n-accent-fg)' }}
      {...props}
    />
  )
}
