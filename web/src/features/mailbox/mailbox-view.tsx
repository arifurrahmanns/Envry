import { useEffect, type ReactNode } from 'react'
import { Inbox } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { toast } from 'sonner'

import { PageHeader } from '@/components/layout/page-header'
import { Toolbar, ToolbarTab } from '@/components/layout/toolbar'
import { EmptyState } from '@/components/mail/empty-state'
import { MailPanes } from '@/components/mail/mail-panes'
import { MessageList } from '@/components/mail/message-list'
import { MessageReader } from '@/components/mail/message-reader'
import { useParam, useSearchQuery } from '@/hooks/use-search-query'
import type { Delivery, Mailbox } from '@/lib/api'
import { DEFAULT_FOLDER, folderIcon, folderLabel, sortFolders } from '@/lib/folders'
import { isDelivery } from '@/lib/messages'
import { useDeleteDelivery, useMailboxMessages, useUpdateDelivery } from '@/lib/queries'
import { neighbourOf } from '@/lib/selection'

type MailboxViewProps = {
  mailbox: Mailbox
  /** Route of this mailbox, e.g. /receivers/dev%40test.local */
  basePath: string
  icon: LucideIcon
  title: ReactNode
  /** Left side of the list header (back link or account switcher). */
  listTitle: ReactNode
}

export function MailboxView({ mailbox, basePath, icon, title, listTitle }: MailboxViewProps) {
  const [q] = useSearchQuery()
  const [folderParam] = useParam('folder')
  const [selected, setSelected] = useParam('m')
  const folders = sortFolders(mailbox.folders)
  const folder = folders.find((f) => f.name === folderParam)?.name ?? DEFAULT_FOLDER

  const messages = useMailboxMessages(mailbox.address, folder, q)
  const update = useUpdateDelivery()
  const deleteOne = useDeleteDelivery()

  const items = (messages.data?.pages.flatMap((p) => p.items) ?? []).filter(isDelivery)
  const total = messages.data?.pages[0]?.total
  const current: Delivery | undefined = items.find((d) => d.id === selected)

  // Opening an unread message marks it read for this mailbox only.
  const { mutate } = update
  useEffect(() => {
    if (current && !current.seen) mutate({ id: current.deliveryId, seen: true })
  }, [current, mutate])

  const remove = async (ids: string[]) => {
    const targets = items.filter((d) => ids.includes(d.id))
    try {
      await Promise.all(targets.map((d) => deleteOne.mutateAsync(d.deliveryId)))
      toast.success(targets.length === 1 ? 'Message deleted' : `${targets.length} messages deleted`)
    } catch (err) {
      toast.error('Delete failed', { description: (err as Error).message })
      throw err
    }
  }

  const folderHref = (name: string) => {
    const p = new URLSearchParams()
    if (name !== DEFAULT_FOLDER) p.set('folder', name)
    const s = p.toString()
    return s ? `${basePath}?${s}` : basePath
  }

  return (
    <>
      <PageHeader icon={icon} title={title} total={total} />
      <Toolbar>
        {folders.map((f) => (
          <ToolbarTab
            key={f.name}
            to={folderHref(f.name)}
            icon={folderIcon(f)}
            label={folderLabel(f.name)}
            count={f.name.toUpperCase() === 'INBOX' ? f.total : undefined}
            active={f.name === folder}
          />
        ))}
      </Toolbar>
      <MailPanes
        list={
          <MessageList
            title={listTitle}
            mode="mailbox"
            items={items}
            selectedId={selected}
            onSelect={setSelected}
            onDelete={remove}
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
              onDelete={() => {
                const next = neighbourOf(items, selected)
                void remove([selected]).then(() => setSelected(next), () => undefined)
              }}
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
