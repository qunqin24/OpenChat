import { type Column } from '@tanstack/react-table'
import { cn } from '@/lib/utils'

type DataTableColumnHeaderProps<TData, TValue> =
  React.HTMLAttributes<HTMLDivElement> & {
    column: Column<TData, TValue>
    title: string
  }

// Upstream (satnaing/shadcn-admin) draws a sort and hide menu here. Every list
// in this panel is ordered and paged by the server and no table has a sorted
// row model, so the menu sorted nothing; hiding columns is the toolbar's
// (view-options.tsx). Only the title is left.
export function DataTableColumnHeader<TData, TValue>({
  title,
  className,
}: DataTableColumnHeaderProps<TData, TValue>) {
  return <div className={cn(className)}>{title}</div>
}
