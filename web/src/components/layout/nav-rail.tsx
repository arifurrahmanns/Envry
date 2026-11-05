import { Link, useMatch } from 'react-router'
import { Plug } from 'lucide-react'

import { Logo } from '@/components/layout/logo'
import { navItems, type NavItem } from '@/components/layout/nav-items'
import { useShell } from '@/components/layout/shell-context'
import { ModeToggle } from '@/components/mode-toggle'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

const itemClass =
  'flex size-10 items-center justify-center rounded-xl border border-transparent text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none'

export function NavRail() {
  const { openConnect } = useShell()

  return (
    <nav aria-label="Main" className="hidden w-[72px] shrink-0 flex-col items-center gap-3 py-4 md:flex">
      <Link
        to="/sent"
        className="mb-5 rounded-[10px] focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        <Logo />
      </Link>

      {navItems.map((item) => (
        <RailLink key={item.to} item={item} />
      ))}

      <div className="mt-auto flex flex-col items-center gap-3">
        <Tooltip>
          <TooltipTrigger asChild>
            <button type="button" className={itemClass} onClick={openConnect} aria-label="Connection settings">
              <Plug className="size-5" strokeWidth={1.75} />
            </button>
          </TooltipTrigger>
          <TooltipContent side="right">Connection settings</TooltipContent>
        </Tooltip>
        <ModeToggle className={itemClass} side="right" />
      </div>
    </nav>
  )
}

function RailLink({ item: { to, label, icon: Icon } }: { item: NavItem }) {
  // Plain string className: Radix Slot (TooltipTrigger asChild) cannot merge
  // NavLink's function form.
  const active = useMatch({ path: to, end: false }) !== null

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Link
          to={to}
          aria-label={label}
          aria-current={active ? 'page' : undefined}
          className={cn(
            itemClass,
            active && 'border-border bg-background text-foreground shadow-sm hover:bg-background dark:bg-accent',
          )}
        >
          <Icon className="size-5" strokeWidth={1.75} />
        </Link>
      </TooltipTrigger>
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  )
}
