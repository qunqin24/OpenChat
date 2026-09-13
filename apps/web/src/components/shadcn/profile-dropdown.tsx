import { useTranslation } from 'react-i18next'
import { Link } from '@tanstack/react-router'
import useDialogState from '@/hooks/use-dialog-state'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/shadcn/ui/avatar'
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
import { initialsOf } from '@/lib/initials'

export function ProfileDropdown() {
  const [open, setOpen] = useDialogState()
  const { data: session } = useSession()
  const { t } = useTranslation()

  return (
    <>
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger asChild>
          <Button variant='ghost' className='relative h-8 w-8 rounded-full'>
            <Avatar className='h-8 w-8'>
              <AvatarImage src={session?.avatarUrl} alt={session?.name ?? ''} />
              <AvatarFallback>{initialsOf(session?.name ?? session?.email)}</AvatarFallback>
            </Avatar>
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
