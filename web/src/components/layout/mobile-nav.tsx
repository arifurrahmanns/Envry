import { useState } from 'react'
import { NavLink } from 'react-router'
import { Menu, Plug } from 'lucide-react'

import { Logo } from '@/components/layout/logo'
import { navItems } from '@/components/layout/nav-items'
import { useShell } from '@/components/layout/shell-context'
import { ModeToggle } from '@/components/mode-toggle'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetDescription, SheetTitle, SheetTrigger } from '@/components/ui/sheet'
import { cn } from '@/lib/utils'

export function MobileNav() {
  const [open, setOpen] = useState(false)
  const { openConnect } = useShell()

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button variant="ghost" size="icon" className="-ml-2 md:hidden" aria-label="Open navigation">
          <Menu className="size-5" />
        </Button>
      </SheetTrigger>
      <SheetContent side="left" className="w-72 gap-0 p-0">
        <div className="flex items-center gap-3 border-b px-5 py-4">
          <Logo />
          <div>
            <SheetTitle className="text-base">Envry</SheetTitle>
            <SheetDescription className="text-xs">Local email testing</SheetDescription>
          </div>
        </div>
        <nav aria-label="Main" className="flex flex-col gap-1 p-3">
          {navItems.map(({ to, label, description, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              onClick={() => setOpen(false)}
              className={({ isActive }) =>
                cn(
                  'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm text-muted-foreground hover:bg-accent hover:text-foreground',
                  isActive && 'bg-muted font-medium text-foreground',
                )
              }
            >
              <Icon className="size-5" strokeWidth={1.75} />
              <span className="flex flex-col">
                {label}
                <span className="text-xs font-normal text-muted-foreground">{description}</span>
              </span>
            </NavLink>
          ))}
        </nav>
        <div className="mt-auto flex items-center gap-2 border-t p-3">
          <Button
            variant="ghost"
            className="flex-1 justify-start gap-3 text-muted-foreground"
            onClick={() => {
              setOpen(false)
              openConnect()
            }}
          >
            <Plug className="size-5" strokeWidth={1.75} />
            Connection settings
          </Button>
          <ModeToggle
            side="bottom"
            className="flex size-9 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
          />
        </div>
      </SheetContent>
    </Sheet>
  )
}
