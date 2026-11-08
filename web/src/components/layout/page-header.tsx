import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { ArrowLeft, MailPlus } from 'lucide-react'

import { MobileNav } from '@/components/layout/mobile-nav'
import { useShell } from '@/components/layout/shell-context'
import { WindowControls } from '@/components/layout/window-controls'
import { Button } from '@/components/ui/button'
import { dragRegion, inDesktopApp, isMac } from '@/lib/desktop'
import { cn } from '@/lib/utils'

type PageHeaderProps = {
  title: ReactNode
  /** Muted text after the title, e.g. a count or the mailbox address. */
  subtitle?: ReactNode
  /** Shows a back arrow linking here. */
  back?: string
  actions?: ReactNode
  /** Show the Compose button (not on the Compose page itself). */
  compose?: boolean
}

export function PageHeader({ title, subtitle, back, actions, compose = true }: PageHeaderProps) {
  const { openCompose } = useShell()
  return (
    // In the desktop app the header doubles as the window's title bar: it is
    // compact, and the window buttons end it on the right.
    <header
      {...dragRegion()}
      className={cn(
        'flex shrink-0 items-center border-b',
        // Desktop: 52px tall with a 12px inset on every side, like the sidebar's logo row.
        inDesktopApp ? cn('h-13 gap-2.5 pl-3', isMac ? 'pr-3' : 'pr-0') : 'h-[84px] gap-3 px-5 md:px-8',
      )}
    >
      <MobileNav />
      {back && (
        <Button asChild variant="ghost" size="icon" className="-ml-2 shrink-0 text-muted-foreground" aria-label="Back">
          <Link to={back}>
            <ArrowLeft className="size-5" />
          </Link>
        </Button>
      )}
      <h1
        className={cn(
          'truncate font-medium tracking-tight',
          inDesktopApp ? 'text-[18px]' : 'text-[24px] md:text-[28px]',
        )}
      >
        {title}
      </h1>
      {subtitle && (
        <span
          className={cn(
            'hidden shrink-0 truncate text-muted-foreground tabular-nums sm:inline',
            inDesktopApp ? 'pt-0.5 text-[13px]' : 'pt-1.5 text-[15px]',
          )}
        >
          {subtitle}
        </span>
      )}
      <div className="ml-auto flex shrink-0 items-center gap-2">
        {actions}
        {compose && (
          <Button
            onClick={() => openCompose()}
            className={cn('gap-2 rounded-lg', inDesktopApp ? 'h-7 px-3 text-[13px]' : 'h-9 px-3.5')}
          >
            <MailPlus className="size-[18px]" strokeWidth={1.75} />
            <span className="hidden sm:inline">Compose</span>
          </Button>
        )}
      </div>
      <WindowControls className="ml-0.5" />
    </header>
  )
}
