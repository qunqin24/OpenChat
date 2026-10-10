import { useTranslation } from 'react-i18next'
import { Link } from '@tanstack/react-router'
import useDialogState from '@/hooks/use-dialog-state'
import { Button } from '@/components/shadcn/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@/components/shadcn/ui/dropdown-menu'
import { SignOutDialog } from '@/components/shadcn/sign-out-dialog'
import { useSession } from '@/features/auth/use-auth'
import { UserAvatar } from '@/components/user-avatar'

export function ProfileDropdown() {
  const [open, setOpen] = useDialogState()
  const { data: session } = useSession()
  const { t } = useTranslation()

  return (
    <>
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger asChild>
          <Button variant='ghost' className='relative h-8 w-8 rounded-full'>
            {/* Deviation from upstream: a coloured initial (UserAvatar) shared with
                the chat, instead of shadcn's Avatar over a fetched picture. */}
            <UserAvatar id={session?.id} name={session?.name} email={session?.email} className='size-8 text-sm' />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className='w-56' align='end' forceMount>
          <DropdownMenuLabel className='font-normal'>
            <div className='flex flex-col gap-1.5'>
              <p className='text-sm leading-none font-medium'>{session?.name}</p>
              <p className='text-xs leading-none text-muted-foreground'>
                {session?.email}
              </p>
            </div>
          </DropdownMenuLabel>
          <DropdownMenuItem asChild>
            <Link to='/admin/settings'>
              {t('admin.ui.menu.settings')}
              <DropdownMenuShortcut>⌘S</DropdownMenuShortcut>
            </Link>
          </DropdownMenuItem>
          <DropdownMenuItem variant='destructive' onClick={() => setOpen(true)}>
            {t('admin.ui.menu.sign-out')}
            <DropdownMenuShortcut className='text-current'>
              ⇧⌘Q
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <SignOutDialog open={!!open} onOpenChange={setOpen} />
    </>
  )
}
