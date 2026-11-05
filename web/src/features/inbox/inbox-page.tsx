import { useState } from 'react'
import { Link, Navigate, useNavigate, useParams } from 'react-router'
import { Check, ChevronDown, Inbox, Plug, Plus, RotateCw } from 'lucide-react'

import { PageHeader } from '@/components/layout/page-header'
import { useShell } from '@/components/layout/shell-context'
import { Toolbar } from '@/components/layout/toolbar'
import { EmptyState } from '@/components/mail/empty-state'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { CreateAccountDialog } from '@/features/inbox/create-account-dialog'
import { MailboxView } from '@/features/mailbox/mailbox-view'
import type { Mailbox } from '@/lib/api'
import { useMailboxes } from '@/lib/queries'

function accountPath(address: string) {
  return `/inbox/${encodeURIComponent(address)}`
}

export function InboxPage() {
  const { data, isLoading, error, refetch } = useMailboxes()
  const { openConnect } = useShell()
  const [creating, setCreating] = useState(false)
  const accounts = data?.items.filter((m) => m.isAccount) ?? []

  if (accounts.length > 0) return <Navigate to={accountPath(accounts[0].address)} replace />

  return (
    <>
      <PageHeader icon={Inbox} title="Inbox" />
      <Toolbar />
      {isLoading ? (
        <div className="flex-1" />
      ) : error ? (
        <EmptyState
          icon={Inbox}
          title="Couldn't load accounts"
          description={error.message}
          action={
            <Button variant="outline" size="sm" onClick={() => void refetch()}>
              <RotateCw />
              Try again
            </Button>
          }
        />
      ) : (
        <EmptyState
          icon={Inbox}
          title="No test accounts yet"
          description="Create an account to read its mail in Thunderbird, Apple Mail or any IMAP or POP3 client, exactly as your users would."
          action={
            <div className="flex flex-wrap justify-center gap-2">
              <Button variant="outline" size="sm" onClick={openConnect}>
                <Plug />
                Connect a client
              </Button>
              <Button size="sm" onClick={() => setCreating(true)}>
                <Plus />
                Create account
              </Button>
            </div>
          }
        />
      )}
      <CreateAccountDialog open={creating} onOpenChange={setCreating} />
    </>
  )
}

export function AccountMailboxPage() {
  const { account = '' } = useParams()
  const { data, isLoading, error } = useMailboxes()
  const accounts = data?.items.filter((m) => m.isAccount) ?? []
  const mailbox = accounts.find((m) => m.address === account.toLowerCase())

  if (!isLoading && !error && !mailbox) return <Navigate to="/inbox" replace />
  if (!mailbox) {
    return (
      <>
        <PageHeader icon={Inbox} title="Inbox" />
        <Toolbar />
        {error && <EmptyState icon={Inbox} title="Couldn't load this account" description={error.message} />}
      </>
    )
  }

  return (
    <MailboxView
      mailbox={mailbox}
      basePath={accountPath(mailbox.address)}
      icon={Inbox}
      title="Inbox"
      listTitle={<AccountSwitcher current={mailbox} accounts={accounts} />}
    />
  )
}

function AccountSwitcher({ current, accounts }: { current: Mailbox; accounts: Mailbox[] }) {
  const [creating, setCreating] = useState(false)
  const navigate = useNavigate()

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger className="-ml-1 flex max-w-full items-center gap-1.5 rounded-md px-1 py-0.5 hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none">
          <span className="truncate">{current.address}</span>
          <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-64">
          {accounts.map((a) => (
            <DropdownMenuItem key={a.address} asChild>
              <Link to={accountPath(a.address)}>
                <span className="min-w-0 flex-1 truncate">{a.address}</span>
                {a.unread > 0 && <span className="text-xs text-muted-foreground tabular-nums">{a.unread}</span>}
                {a.address === current.address && <Check />}
              </Link>
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => setCreating(true)}>
            <Plus />
            New account
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <CreateAccountDialog
        open={creating}
        onOpenChange={setCreating}
        onCreated={(address) => navigate(accountPath(address))}
      />
    </>
  )
}
