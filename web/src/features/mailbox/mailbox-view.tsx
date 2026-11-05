import { useEffect, type ReactNode } from 'react'
import { CheckCheck, Inbox, Plug } from 'lucide-react'
import { toast } from 'sonner'

import { PageHeader } from '@/components/layout/page-header'
import { useShell } from '@/components/layout/shell-context'
import { EmptyState } from '@/components/mail/empty-state'
import { ListTab } from '@/components/mail/list-tab'
import { MailPanes } from '@/components/mail/mail-panes'
import { MessageList } from '@/components/mail/message-list'
import { MessageReader } from '@/components/mail/message-reader'
import { DropdownMenuItem } from '@/components/ui/dropdown-menu'
import { useParam, useSearchQuery } from '@/hooks/use-search-query'
import type { Delivery, Mailbox } from '@/lib/api'
import { DEFAULT_FOLDER, folderLabel, sortFolders } from '@/lib/folders'
import { isDelivery } from '@/lib/messages'
import { useDeleteDelivery, useMailboxMessages, useUpdateDelivery } from '@/lib/queries'
import { neighbourOf } from '@/lib/selection'

type MailboxViewProps = {
  mailbox: Mailbox
  /** Route of this mailbox, e.g. /receivers/dev%40test.local */
  basePath: string
  title: ReactNode
  subtitle?: ReactNode
  back?: string
}

export function MailboxView({ mailbox, basePath, title, subtitle, back }: MailboxViewProps) {
  const [q] = useSearchQuery()
  const [folderParam] = useParam('folder')
  const [selected, setSelected] = useParam('m')
  const { openConnect } = useShell()
  const folders = sortFolders(mailbox.folders)
  const folder = folders.find((f) => f.name === folderParam)?.name ?? DEFAULT_FOLDER

  const messages = useMailboxMessages(mailbox.address, folder, q)
  const update = useUpdateDelivery()
  const deleteOne = useDeleteDelivery()

  const items = (messages.data?.pages.flatMap((p) => p.items) ?? []).filter(isDelivery)
  const current: Delivery | undefined = items.find((d) => d.id === selected)

  // Opening an unread message marks it read for this mailbox only.
  const { mutate } = update
  useEffect(() => {
    if (current && !current.seen) mutate({ id: current.deliveryId, seen: true })
  }, [current, mutate])

  const byId = (id: string) => items.find((d) => d.id === id)

  const remove = (id: string) => {
    const d = byId(id)
    if (!d) return
    const next = id === selected ? neighbourOf(items, id) : selected
    deleteOne.mutate(d.deliveryId, {
      onSuccess: () => {
        toast.success('Message deleted')
        setSelected(next)
      },
      onError: (err) => toast.error('Delete failed', { description: err.message }),
    })
  }

  const toggleSeen = (id: string) => {
    const d = byId(id)
    if (d) update.mutate({ id: d.deliveryId, seen: !d.seen })
  }

  const markAllRead = () => {
    const unread = items.filter((d) => !d.seen)
    for (const d of unread) update.mutate({ id: d.deliveryId, seen: true })
    toast.success(unread.length === 0 ? 'Nothing to mark' : `Marked ${unread.length} as read`)
  }

  const folderHref = (name: string) => {
    if (name === DEFAULT_FOLDER) return basePath
    return `${basePath}?${new URLSearchParams({ folder: name })}`
  }

  return (
    <>
      <PageHeader title={title} subtitle={subtitle} back={back} />
      <MailPanes
        list={
          <MessageList
            tabs={folders.map((f) => (
              <ListTab
                key={f.name}
                to={folderHref(f.name)}
                label={folderLabel(f.name)}
                count={f.name === DEFAULT_FOLDER ? f.total : undefined}
                active={f.name === folder}
              />
            ))}
            menu={
              <>
                <DropdownMenuItem onSelect={markAllRead}>
                  <CheckCheck />
                  Mark all as read
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={openConnect}>
                  <Plug />
                  Connection settings
                </DropdownMenuItem>
              </>
            }
            mode="mailbox"
            items={items}
            selectedId={selected}
            onSelect={setSelected}
            onDelete={remove}
            onToggleSeen={toggleSeen}
            isLoading={messages.isLoading}
            error={messages.error}
            onRetry={() => void messages.refetch()}
            hasNextPage={messages.hasNextPage}
            isFetchingNextPage={messages.isFetchingNextPage}
            fetchNextPage={() => void messages.fetchNextPage()}
            className="flex-1"
            empty={
              <EmptyState
                icon={Inbox}
                title={q ? 'No matches' : `${folderLabel(folder)} is empty`}
                description={
                  q
                    ? `Nothing in ${folderLabel(folder)} matches “${q}”.`
                    : folder === DEFAULT_FOLDER
                      ? `Mail sent to ${mailbox.address} lands here.`
                      : undefined
                }
              />
            }
          />
        }
        reader={
          selected ? (
            <MessageReader
              id={selected}
              delivery={current}
              viewingAs={mailbox.address}
              onBack={() => setSelected(null)}
              onDelete={() => remove(selected)}
              deleting={deleteOne.isPending}
              onToggleSeen={
                current
                  ? () => {
                      update.mutate({ id: current.deliveryId, seen: !current.seen })
                      if (current.seen) setSelected(null)
                    }
                  : undefined
              }
            />
          ) : null
        }
      />
    </>
  )
}
