import { Link, useLocation, useNavigate } from '@tanstack/react-router'
import { useState, type JSX } from 'react'

import { cn } from '@/lib/utils'
import { buttonVariants } from '@/components/shadcn/ui/button'
import { ScrollArea } from '@/components/shadcn/ui/scroll-area'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/shadcn/ui/select'

type SidebarNavProps = React.HTMLAttributes<HTMLElement> & {
  items: {
    href: string
    title: string
    icon: JSX.Element
  }[]
}

/** Vertical nav on md and up, collapsed into a select below it. */
export function SidebarNav({ className, items, ...props }: SidebarNavProps) {
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const [value, setValue] = useState(pathname)

  const handleSelect = (href: string) => {
    setValue(href)
    void navigate({ to: href })
  }

  return (
    <>
      <div className="p-1 md:hidden">
        <Select value={value} onValueChange={handleSelect}>
          <SelectTrigger className="h-12 sm:w-48">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {items.map((item) => (
              <SelectItem key={item.href} value={item.href}>
                <div className="flex gap-x-4 px-2 py-1">
                  <span className="scale-125">{item.icon}</span>
                  <span className="text-md">{item.title}</span>
                </div>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <ScrollArea
        orientation="horizontal"
        type="always"
        className="bg-background hidden w-full min-w-40 px-1 py-2 md:block"
      >
        <nav
          className={cn(
            'flex space-x-2 py-1 lg:flex-col lg:space-y-1 lg:space-x-0',
            className,
          )}
          {...props}
        >
          {items.map((item) => (
            <Link
              key={item.href}
              to={item.href}
              className={cn(
                buttonVariants({ variant: 'ghost' }),
                pathname === item.href ? 'bg-muted hover:bg-accent' : 'hover:bg-accent',
                'justify-start',
              )}
            >
              <span className="me-2">{item.icon}</span>
              {item.title}
            </Link>
          ))}
        </nav>
      </ScrollArea>
    </>
  )
}
