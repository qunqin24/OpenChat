import { Separator } from '@/components/shadcn/ui/separator'

type ContentSectionProps = {
  title: string
  desc: string
  children: React.JSX.Element
}

export function ContentSection({ title, desc, children }: ContentSectionProps) {
  return (
    <div className="flex flex-1 flex-col">
      <div className="flex-none">
        <h3 className="text-lg font-medium">{title}</h3>
        <p className="text-muted-foreground text-sm">{desc}</p>
      </div>
      <Separator className="my-4 flex-none" />
      {/* `relative`, unlike upstream: Radix checkboxes and selects keep a hidden
          native input absolutely positioned beside them, and without a
          positioned ancestor here they are placed against the page's layout,
          below this scroller, making the whole page scroll as well (the
          moderation tab's category list did). */}
      <div className="faded-bottom relative h-full w-full overflow-y-auto scroll-smooth motion-reduce:scroll-auto pb-12 pe-4">
        <div className="-mx-1 px-1.5 lg:max-w-xl">{children}</div>
      </div>
    </div>
  )
}
