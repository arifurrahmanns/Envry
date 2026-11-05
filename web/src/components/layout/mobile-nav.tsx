import { useState } from 'react'
import { Menu } from 'lucide-react'

import { SidebarContent } from '@/components/layout/sidebar'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetDescription, SheetTitle, SheetTrigger } from '@/components/ui/sheet'

export function MobileNav() {
  const [open, setOpen] = useState(false)
  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button variant="ghost" size="icon" className="-ml-2 shrink-0 md:hidden" aria-label="Open navigation">
          <Menu className="size-5" />
        </Button>
      </SheetTrigger>
      <SheetContent side="left" className="w-[284px] gap-0 bg-sidebar p-0">
        <SheetTitle className="sr-only">Envry</SheetTitle>
        <SheetDescription className="sr-only">Navigation</SheetDescription>
        <SidebarContent onNavigate={() => setOpen(false)} />
      </SheetContent>
    </Sheet>
  )
}
