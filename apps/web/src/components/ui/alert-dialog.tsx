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
          'fixed top-1/2 left-1/2 z-50 w-[min(30rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2',
          // OpenWebUI's confirm dialog: generous corners and padding, a heading
          // that asks, and two full-width pill buttons side by side.
          'rounded-3xl p-6',
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
  return <div className={cn('flex flex-col gap-2', className)} {...props} />
}

export function AlertDialogFooter({ className, ...props }: ComponentProps<'div'>) {
  return <div className={cn('mt-6 grid grid-cols-2 gap-2.5', className)} {...props} />
}

export function AlertDialogTitle({
  className,
  ...props
}: ComponentProps<typeof RadixAlertDialog.Title>) {
  return (
    <RadixAlertDialog.Title
      className={cn('text-[17px] leading-6 font-semibold', className)}
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
      className={cn(
        'h-10 rounded-full px-4 text-sm font-medium transition-colors',
        'bg-[var(--n-hover-strong)] text-[var(--n-text)] hover:bg-[var(--n-active)]',
        // The portal sits outside the chat's own focus styles (focus.css); a soft
        // ring rather than the browser's blue outline, which clashed here.
        'outline-none focus-visible:ring-[3px] focus-visible:ring-black/15 dark:focus-visible:ring-white/25',
        className,
      )}
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
      className={cn(
        'h-10 rounded-full px-4 text-sm font-medium transition-colors',
        'bg-[var(--n-accent)] text-[var(--n-accent-fg)] hover:bg-[var(--n-accent-hover)]',
        'outline-none focus-visible:ring-[3px] focus-visible:ring-black/15 dark:focus-visible:ring-white/25',
        className,
      )}
      {...props}
    />
  )
}
