import { Toaster as Sonner, type ToasterProps } from 'sonner'
import { useUIStore } from '@/stores/ui-store'

export function Toaster({ ...props }: ToasterProps) {
  // Sourced from our own store rather than shadcn's ThemeProvider.
  const theme = useUIStore((s) => s.theme)

  return (
    <Sonner
      theme={theme as ToasterProps['theme']}
      className='toaster group [&_div[data-content]]:w-full'
      style={
        {
          '--normal-bg': 'var(--popover)',
          '--normal-text': 'var(--popover-foreground)',
          '--normal-border': 'var(--border)',
        } as React.CSSProperties
      }
      {...props}
    />
  )
}
