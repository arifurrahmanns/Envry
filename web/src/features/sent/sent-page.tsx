import { useState } from 'react'
import { Inbox, Plug, Trash2 } from 'lucide-react'
import { toast } from 'sonner'

import { PageHeader } from '@/components/layout/page-header'
import { useShell } from '@/components/layout/shell-context'
import { EmptyState } from '@/components/mail/empty-state'
import { ListTab } from '@/components/mail/list-tab'
import { MailPanes } from '@/components/mail/mail-panes'
import { MessageList } from '@/components/mail/message-list'
import { MessageReader } from '@/components/mail/message-reader'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { DropdownMenuItem, DropdownMenuSeparator } from '@/components/ui/dropdown-menu'
import { useParam, useSearchQuery } from '@/hooks/use-search-query'
import { count } from '@/lib/format'
import { useDeleteAllMessages, useDeleteMessage, useMessages } from '@/lib/queries'
import { neighbourOf } from '@/lib/selection'

export function SentPage() {
  const [q] = useSearchQuery()
  const [selected, setSelected] = useParam('m')
  const { openCompose, openConnect } = useShell()
  const [clearing, setClearing] = useState(false)
  const messages = useMessages(q)
  const deleteOne = useDeleteMessage()

  const items = messages.data?.pages.flatMap((p) => p.items) ?? []
  const total = messages.data?.pages[0]?.total

  const remove = (id: string) => {
    const next = id === selected ? neighbourOf(items, id) : selected
    deleteOne.mutate(id, {
      onSuccess: () => {
        toast.success('Message deleted')
        setSelected(next)
      },
      onError: (err) => toast.error('Delete failed', { description: err.message }),
    })
  }

  return (
    <>
      <PageHeader title="Sent" subtitle={total !== undefined ? `${count(total)} ${total === 1 ? 'email' : 'emails'}` : undefined} />
      <MailPanes
        list={
          <MessageList
            tabs={<ListTab to="/sent" label="All mail" count={total} active />}
            menu={
              <>
                <DropdownMenuItem onSelect={openConnect}>
                  <Plug />
                  Connection settings
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" disabled={items.length === 0} onSelect={() => setClearing(true)}>
                  <Trash2 />
                  Delete all messages
                </DropdownMenuItem>
              </>
            }
            mode="sent"
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
              q ? (
                <EmptyState icon={Inbox} title="No matches" description={`Nothing matches “${q}”.`} />
              ) : (
                <EmptyState
                  icon={Inbox}
                  title="No emails yet"
                  description="Send an email over SMTP from your app and it shows up here instantly."
                  action={
                    <div className="flex flex-wrap justify-center gap-2">
                      <Button variant="outline" size="sm" onClick={openConnect}>
                        <Plug />
                        Connection settings
                      </Button>
                      <Button size="sm" onClick={() => openCompose()}>
                        Send a test email
                      </Button>
                    </div>
                  }
                />
              )
            }
          />
        }
        reader={
          selected ? (
            <MessageReader
              id={selected}
              onBack={() => setSelected(null)}
              onDelete={() => remove(selected)}
              deleting={deleteOne.isPending}
            />
          ) : null
        }
      />
      <ClearAllDialog open={clearing} onOpenChange={setClearing} />
    </>
  )
}

function ClearAllDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const clear = useDeleteAllMessages()
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete every captured email?</AlertDialogTitle>
          <AlertDialogDescription>
            This removes all messages from Sent, every recipient mailbox and every test account. It can't be undone.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            className="bg-destructive text-white hover:bg-destructive/90"
            onClick={() =>
              clear.mutate(undefined, {
                onSuccess: () => toast.success('All messages deleted'),
                onError: (err) => toast.error('Delete failed', { description: err.message }),
              })
            }
          >
            Delete all
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
