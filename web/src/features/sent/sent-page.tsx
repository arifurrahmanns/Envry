import { useState } from 'react'
import { Inbox, Mails, Plug, Send, Trash2 } from 'lucide-react'
import { toast } from 'sonner'

import { PageHeader } from '@/components/layout/page-header'
import { useShell } from '@/components/layout/shell-context'
import { Toolbar, ToolbarTab } from '@/components/layout/toolbar'
import { ConnectBanner } from '@/components/mail/connect-banner'
import { EmptyState } from '@/components/mail/empty-state'
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
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { useParam, useSearchQuery } from '@/hooks/use-search-query'
import { neighbourOf } from '@/lib/selection'
import { useDeleteAllMessages, useDeleteMessage, useMessages } from '@/lib/queries'

export function SentPage() {
  const [q] = useSearchQuery()
  const [selected, setSelected] = useParam('m')
  const { openCompose, openConnect } = useShell()
  const messages = useMessages(q)
  const deleteOne = useDeleteMessage()

  const items = messages.data?.pages.flatMap((p) => p.items) ?? []
  const total = messages.data?.pages[0]?.total

  const remove = async (ids: string[]) => {
    try {
      await Promise.all(ids.map((id) => deleteOne.mutateAsync(id)))
      toast.success(ids.length === 1 ? 'Message deleted' : `${ids.length} messages deleted`)
    } catch (err) {
      toast.error('Delete failed', { description: (err as Error).message })
      throw err
    }
  }

  const deleteSelected = (id: string) => {
    const next = neighbourOf(items, id)
    void remove([id]).then(() => setSelected(next), () => undefined)
  }

  return (
    <>
      <PageHeader icon={Send} title="Sent" total={total} />
      <Toolbar actions={items.length > 0 && <ClearAll />}>
        <ToolbarTab to="/sent" icon={Mails} label="All mail" count={total} active />
      </Toolbar>
      <MailPanes
        list={
          <MessageList
            title={q ? `Results for “${q}”` : 'All mail'}
            banner={!q && <ConnectBanner />}
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
              showEnvelope
              onBack={() => setSelected(null)}
              onDelete={() => deleteSelected(selected)}
              deleting={deleteOne.isPending}
            />
          ) : null
        }
      />
    </>
  )
}

function ClearAll() {
  const [open, setOpen] = useState(false)
  const clear = useDeleteAllMessages()

  return (
    <AlertDialog open={open} onOpenChange={setOpen}>
      <AlertDialogTrigger asChild>
        <Button variant="ghost" className="h-9 gap-2 text-muted-foreground">
          <Trash2 className="size-4" />
          <span className="hidden sm:inline">Clear all</span>
        </Button>
      </AlertDialogTrigger>
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
                onError: (err) => toast.error('Clear failed', { description: err.message }),
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
