import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { ArrowLeft, MailPlus } from 'lucide-react'

import { MobileNav } from '@/components/layout/mobile-nav'
import { useShell } from '@/components/layout/shell-context'
import { Button } from '@/components/ui/button'

type PageHeaderProps = {
  title: ReactNode
  /** Muted text after the title, e.g. a count or the mailbox address. */
  subtitle?: ReactNode
  /** Shows a back arrow linking here. */
  back?: string
  actions?: ReactNode
}

export function PageHeader({ title, subtitle, back, actions }: PageHeaderProps) {
  const { openCompose } = useShell()
  return (
    <header className="flex h-[84px] shrink-0 items-center gap-3 border-b px-5 md:px-8">
      <MobileNav />
      {back && (
        <Button asChild variant="ghost" size="icon" className="-ml-2 shrink-0 text-muted-foreground" aria-label="Back">
          <Link to={back}>
            <ArrowLeft className="size-5" />
          </Link>
        </Button>
      )}
      <h1 className="truncate text-[24px] font-medium tracking-tight md:text-[28px]">{title}</h1>
      {subtitle && (
        <span className="hidden shrink-0 truncate pt-1.5 text-[15px] text-muted-foreground tabular-nums sm:inline">
          {subtitle}
        </span>
      )}
      <div className="ml-auto flex shrink-0 items-center gap-2">
        {actions}
        <Button onClick={() => openCompose()} className="h-9 gap-2 rounded-lg px-3.5">
          <MailPlus className="size-[18px]" strokeWidth={1.75} />
          <span className="hidden sm:inline">Compose</span>
        </Button>
      </div>
    </header>
  )
}
