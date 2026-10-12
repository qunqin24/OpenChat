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
      <div className="faded-bottom h-full w-full overflow-y-auto scroll-smooth motion-reduce:scroll-auto pb-12 pe-4">
        {/* `relative`, unlike upstream: Radix checkboxes and selects keep a
            hidden native input absolutely positioned beside them, and without
            a positioned ancestor inside this scroller they are placed against
            the page's layout below it, making the whole page scroll as well
            (the moderation tab's category list did). On this inner box, not
            the scroller: `faded-bottom` draws its fade against the scroller's
            positioned ancestor, and a positioned scroller would make the fade
            scroll with the content, over the text in the middle. */}
        <div className="relative -mx-1 px-1.5 lg:max-w-xl">{children}</div>
      </div>
    </div>
  )
}
