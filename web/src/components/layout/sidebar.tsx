import type { ReactNode } from 'react'
import { Link, useMatch } from 'react-router'
import { Inbox, Monitor, Moon, PanelLeftClose, PanelLeftOpen, Plug, Plus, Search, Send, Sun, Users } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

import { Logo } from '@/components/layout/logo'
import { SearchBox } from '@/components/layout/search-box'
import { useShell } from '@/components/layout/shell-context'
import { SenderAvatar } from '@/components/mail/sender-avatar'
import { useTheme, type Theme } from '@/components/theme-provider'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { dragRegion, inDesktopApp, isMac } from '@/lib/desktop'
import { count } from '@/lib/format'
import { useMailboxes } from '@/lib/queries'
import { cn } from '@/lib/utils'

/** The desktop app's compact header (52px) leaves less room above the logo. */
const compactTop = inDesktopApp && !isMac

type SidebarProps = {
  collapsed?: boolean
  onToggle?: () => void
  /** Called after navigating, e.g. to close the mobile sheet. */
  onNavigate?: () => void
  /** Show the active marker on the sidebar's right border. */
  marker?: boolean
}

/** The sidebar on wider screens; it fills its resizable pane (see AppShell). */
export function Sidebar({ collapsed = false, onToggle }: { collapsed?: boolean; onToggle: () => void }) {
  return (
    <aside className="flex h-full w-full flex-col bg-sidebar">
      <SidebarContent collapsed={collapsed} onToggle={onToggle} marker />
    </aside>
  )
}

export function SidebarContent({ collapsed = false, onToggle, onNavigate, marker = false }: SidebarProps) {
  const { openCreateAccount } = useShell()
  const { data } = useMailboxes()
  const accounts = (data?.items ?? []).filter((m) => m.isAccount)
  const inboxUnread = accounts.reduce((n, a) => n + (a.folders.find((f) => f.name === 'INBOX')?.unread ?? 0), 0)

  return (
    // In the desktop app the sidebar's empty space and logo row move the
    // window, and the logo row lines up with the compact page header (except
    // on macOS, where the traffic lights need the space above it).
    <div
      {...dragRegion(false)}
      className={cn(
        'flex h-full min-h-0 flex-col pb-5',
        // Desktop: one 12px inset, so the logo sits 12px from the top, the
        // left and the header line below it (52px header, 28px logo).
        compactTop ? 'pt-2.5' : 'pt-8',
        collapsed ? 'items-center px-3' : compactTop ? 'px-3' : 'px-5',
      )}
    >
      {collapsed && onToggle ? (
        <div {...dragRegion()} className={compactTop ? 'mb-4' : 'mb-7'}>
          <ExpandToggle onClick={onToggle} />
        </div>
      ) : (
        <div {...dragRegion()} className={cn('flex items-center justify-between', compactTop ? 'mb-4' : 'mb-7 pl-1')}>
          <Link
            to="/sent"
            onClick={onNavigate}
            className="rounded-md focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
          >
            <Logo />
          </Link>
          {onToggle && <IconAction icon={PanelLeftClose} label="Collapse sidebar" onClick={onToggle} />}
        </div>
      )}

      {collapsed ? (
        <IconAction icon={Search} label="Search" onClick={onToggle} className="mb-4 size-10 rounded-xl border" />
      ) : (
        <SearchBox className="mb-5" />
      )}

      <nav aria-label="Main" className={cn('flex flex-col gap-0.5', collapsed && 'items-center')}>
        <NavItem to="/sent" icon={Send} label="Sent" collapsed={collapsed} marker={marker} onNavigate={onNavigate} />
        <NavItem to="/receivers" icon={Users} label="Receivers" collapsed={collapsed} marker={marker} onNavigate={onNavigate} />
      </nav>

      <Divider />

      <nav aria-label="Mailbox" className={cn('flex flex-col gap-0.5', collapsed && 'items-center')}>
        <NavItem
          to="/inbox"
          icon={Inbox}
          label="Inbox"
          badge={inboxUnread}
          collapsed={collapsed}
          marker={marker}
          onNavigate={onNavigate}
        />
        <NavItem
          to="/connection"
          icon={Plug}
          label="Connection"
          collapsed={collapsed}
          marker={marker}
          onNavigate={onNavigate}
        />
        <AppearanceItem collapsed={collapsed} />
      </nav>

      <Divider />

      <div className={cn('flex min-h-0 flex-1 flex-col', collapsed && 'items-center')}>
        {!collapsed && (
          <div className="mb-1 flex items-center justify-between pr-1 pl-3">
            <span className="text-[13px] text-muted-foreground">Accounts</span>
            <IconAction
              icon={Plus}
              label="New account"
              onClick={() => {
                onNavigate?.()
                openCreateAccount()
              }}
              className="size-7"
            />
          </div>
        )}
        <div className={cn('-mx-1 flex min-h-0 flex-col gap-0.5 overflow-y-auto px-1', collapsed && 'items-center')}>
          {accounts.map((a) => (
            <AccountItem
              key={a.address}
              address={a.address}
              name={a.displayName}
              unread={a.folders.find((f) => f.name === 'INBOX')?.unread ?? 0}
              collapsed={collapsed}
              onNavigate={onNavigate}
            />
          ))}
          {accounts.length === 0 && !collapsed && (
            <button
              type="button"
              onClick={() => {
                onNavigate?.()
                openCreateAccount()
              }}
              className="rounded-[10px] px-3 py-2 text-left text-[13px] text-muted-foreground hover:bg-accent hover:text-foreground"
            >
              Add a test account to read mail over IMAP and POP3.
            </button>
          )}
          {collapsed && <IconAction icon={Plus} label="New account" onClick={openCreateAccount} />}
        </div>
      </div>

      <LiveStatus collapsed={collapsed} />
    </div>
  )
}

function Divider() {
  return <div aria-hidden className="my-5 h-px w-full shrink-0 bg-border" />
}

/** In the collapsed sidebar the logo doubles as the expand button: it
 *  shows the logo, and the expand icon on hover or keyboard focus. */
function ExpandToggle({ onClick }: { onClick: () => void }) {
  return (
    <WithTooltip label="Expand sidebar">
      <button
        type="button"
        onClick={onClick}
        aria-label="Expand sidebar"
        className="group relative flex size-10 items-center justify-center rounded-[10px] transition-colors hover:bg-accent focus-visible:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        <Logo className="transition-opacity duration-150 group-hover:opacity-0 group-focus-visible:opacity-0" />
        <PanelLeftOpen
          aria-hidden
          className="absolute size-5 text-foreground opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-visible:opacity-100"
          strokeWidth={1.75}
        />
      </button>
    </WithTooltip>
  )
}

const itemBase =
  'relative flex h-10 items-center gap-3 rounded-[10px] border border-transparent text-[14px] text-foreground/85 transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none'

function NavItem({
  to,
  icon: Icon,
  label,
  badge = 0,
  collapsed,
  marker,
  onNavigate,
}: {
  to: string
  icon: LucideIcon
  label: string
  badge?: number
  collapsed: boolean
  marker: boolean
  onNavigate?: () => void
}) {
  const active = useMatch({ path: to, end: false }) !== null
  const link = (
    <Link
      to={to}
      onClick={onNavigate}
      aria-current={active ? 'page' : undefined}
      aria-label={collapsed ? label : undefined}
      className={cn(
        itemBase,
        collapsed ? 'size-10 justify-center' : 'w-full px-3',
        active && 'border-border bg-accent font-medium text-foreground shadow-xs',
      )}
    >
      <Icon className="size-[18px] shrink-0 text-muted-foreground" strokeWidth={1.75} />
      {!collapsed && <span className="truncate">{label}</span>}
      {badge > 0 &&
        (collapsed ? (
          <span className="absolute top-1.5 right-1.5 size-2 rounded-full bg-brand" />
        ) : (
          <span className="ml-auto min-w-6 rounded-md bg-brand px-1.5 text-center text-[12px] leading-[22px] font-semibold text-brand-foreground tabular-nums">
            {badge > 99 ? '99+' : badge}
          </span>
        ))}
      {active && marker && (
        <span
          aria-hidden
          className={cn('absolute top-1/2 h-5 w-[3px] -translate-y-1/2 rounded-l-full bg-foreground', collapsed || compactTop ? '-right-[13px]' : '-right-[21px]')}
        />
      )}
    </Link>
  )
  return collapsed ? <WithTooltip label={label}>{link}</WithTooltip> : link
}

const themes: { value: Theme; label: string; icon: LucideIcon }[] = [
  { value: 'light', label: 'Light', icon: Sun },
  { value: 'dark', label: 'Dark', icon: Moon },
  { value: 'system', label: 'System', icon: Monitor },
]

function AppearanceItem({ collapsed }: { collapsed: boolean }) {
  const { theme, resolvedTheme, setTheme } = useTheme()
  const Icon = resolvedTheme === 'dark' ? Moon : Sun
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label="Appearance"
        className={cn(itemBase, collapsed ? 'size-10 justify-center' : 'w-full px-3 text-left')}
      >
        <Icon className="size-[18px] shrink-0 text-muted-foreground" strokeWidth={1.75} />
        {!collapsed && (
          <>
            Appearance
            <span className="ml-auto text-[13px] text-muted-foreground capitalize">{theme}</span>
          </>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent side={collapsed ? 'right' : 'bottom'} align="start" className="w-44">
        {themes.map(({ value, label, icon: ItemIcon }) => (
          <DropdownMenuItem key={value} onSelect={() => setTheme(value)} className={cn(theme === value && 'font-medium')}>
            <ItemIcon />
            {label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function AccountItem({
  address,
  name,
  unread,
  collapsed,
  onNavigate,
}: {
  address: string
  name: string | null
  unread: number
  collapsed: boolean
  onNavigate?: () => void
}) {
  const to = `/inbox/${encodeURIComponent(address)}`
  const active = useMatch({ path: to, end: false }) !== null
  const link = (
    <Link
      to={to}
      onClick={onNavigate}
      aria-label={collapsed ? address : undefined}
      className={cn(
        itemBase,
        'h-10',
        collapsed ? 'size-10 justify-center' : 'w-full pr-3 pl-2.5',
        active && 'bg-accent text-foreground',
      )}
    >
      <SenderAvatar name={name ?? ''} address={address} size="sm" />
      {!collapsed && (
        <>
          <span className="min-w-0 flex-1 truncate">{name || address}</span>
          {unread > 0 && <span className="text-[12px] text-muted-foreground tabular-nums">{count(unread)}</span>}
        </>
      )}
    </Link>
  )
  return collapsed ? <WithTooltip label={address}>{link}</WithTooltip> : link
}

function LiveStatus({ collapsed }: { collapsed: boolean }) {
  const { live } = useShell()
  const dot = (
    <span
      aria-hidden
      className={cn(
        'size-2 shrink-0 rounded-full',
        live === 'live' ? 'bg-success' : live === 'offline' ? 'bg-muted-foreground/50' : 'animate-pulse bg-success/50',
      )}
    />
  )
  const label = live === 'live' ? 'Live' : live === 'offline' ? 'Offline' : 'Connecting'
  if (collapsed) {
    return (
      <WithTooltip label={label}>
        <span className="mt-4 flex size-10 items-center justify-center">{dot}</span>
      </WithTooltip>
    )
  }
  return (
    <div className="mt-4 flex items-center gap-2 px-3 text-[12px] text-muted-foreground" aria-live="polite">
      {dot}
      <span>{label}</span>
    </div>
  )
}

function IconAction({
  icon: Icon,
  label,
  onClick,
  className,
}: {
  icon: LucideIcon
  label: string
  onClick?: () => void
  className?: string
}) {
  return (
    <WithTooltip label={label}>
      <button
        type="button"
        onClick={onClick}
        aria-label={label}
        className={cn(
          'flex size-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
          className,
        )}
      >
        <Icon className="size-[18px]" strokeWidth={1.75} />
      </button>
    </WithTooltip>
  )
}

function WithTooltip({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  )
}
