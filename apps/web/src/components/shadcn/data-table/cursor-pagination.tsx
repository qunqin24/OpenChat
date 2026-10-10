import { useTranslation } from 'react-i18next'
import { ChevronLeftIcon, ChevronRightIcon, DoubleArrowLeftIcon } from '@radix-ui/react-icons'
import { cn } from '@/lib/utils'
import { Button } from '@/components/shadcn/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/shadcn/ui/select'

type DataTableCursorPaginationProps = {
  /** One-based, among the pages visited so far. */
  page: number
  pageSize: number
  onPageSizeChange: (size: number) => void
  canPrevious: boolean
  canNext: boolean
  onFirst: () => void
  onPrevious: () => void
  onNext: () => void
  className?: string
}

/**
 * The pager of a table paged by cursor (the usage ledger). A cursor leads one
 * page forward and says nothing about how many pages there are, so — unlike
 * DataTablePagination — there are no page numbers and no last page: the first
 * page, the previous one, the next one, and which page this is. The previous
 * pages are the cursors already visited, kept by the caller.
 */
export function DataTableCursorPagination({
  page,
  pageSize,
  onPageSizeChange,
  canPrevious,
  canNext,
  onFirst,
  onPrevious,
  onNext,
  className,
}: DataTableCursorPaginationProps) {
  const { t } = useTranslation()

  return (
    <div
      className={cn(
        'flex items-center justify-between overflow-clip px-2',
        '@max-2xl/content:flex-col-reverse @max-2xl/content:gap-4',
        className
      )}
      style={{ overflowClipMargin: 1 }}
    >
      <div className='flex items-center gap-2'>
        <Select value={`${pageSize}`} onValueChange={(value) => onPageSizeChange(Number(value))}>
          <SelectTrigger className='h-8 w-17.5'>
            <SelectValue placeholder={pageSize} />
          </SelectTrigger>
          <SelectContent side='top'>
            {[10, 20, 30, 40, 50].map((size) => (
              <SelectItem key={size} value={`${size}`}>
                {size}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <p className='hidden text-sm font-medium sm:block'>{t('admin.ui.table.rows-per-page')}</p>
      </div>

      <div className='flex items-center space-x-2 sm:space-x-4'>
        <div className='flex min-w-20 items-center justify-center text-sm font-medium'>
          {t('admin.ui.table.page', { page })}
        </div>
        <div className='flex items-center space-x-2'>
          <Button variant='outline' className='size-8 p-0 @max-md/content:hidden' onClick={onFirst} disabled={!canPrevious}>
            <span className='sr-only'>{t('admin.ui.table.first-page')}</span>
            <DoubleArrowLeftIcon className='h-4 w-4' />
          </Button>
          <Button variant='outline' className='size-8 p-0' onClick={onPrevious} disabled={!canPrevious}>
            <span className='sr-only'>{t('admin.ui.table.previous-page')}</span>
            <ChevronLeftIcon className='h-4 w-4' />
          </Button>
          <Button variant='outline' className='size-8 p-0' onClick={onNext} disabled={!canNext}>
            <span className='sr-only'>{t('admin.ui.table.next-page')}</span>
            <ChevronRightIcon className='h-4 w-4' />
          </Button>
        </div>
      </div>
    </div>
  )
}
